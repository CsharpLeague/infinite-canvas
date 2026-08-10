package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	canvasAgentMaxSteps       = 16
	canvasAgentMaxCallAttempts = 3
)

var canvasAgentRunLocks sync.Map
var canvasAgentRunCancels sync.Map

type CreateCanvasAgentRunInput struct {
	SessionID     string          `json:"sessionId"`
	CanvasID      string          `json:"canvasId"`
	SkillID       string          `json:"skillId"`
	Phase         string          `json:"phase"`
	Model         string          `json:"model"`
	ChannelID     string          `json:"channelId"`
	UserChannelID string          `json:"userChannelId"`
	Message       string          `json:"message"`
	CanvasContext json.RawMessage `json:"canvasContext"`
	Input         json.RawMessage `json:"input"`
}

type CanvasAgentToolResultInput struct {
	CallID string          `json:"callId"`
	Name   string          `json:"name"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func CreateCanvasAgentRun(ctx context.Context, input CreateCanvasAgentRunInput) (model.CanvasAgentRun, error) {
	if strings.TrimSpace(input.SessionID) == "" {
		return model.CanvasAgentRun{}, errors.New("会话 ID 不能为空")
	}
	if strings.TrimSpace(input.Message) == "" {
		return model.CanvasAgentRun{}, errors.New("Agent 输入不能为空")
	}
	modelName, err := workflowDraftModel(input.Model)
	if err != nil {
		return model.CanvasAgentRun{}, err
	}
	var skill model.CanvasSkill
	if input.SkillID != "" {
		skill, err = repository.FindCanvasSkill(input.SkillID, true)
		if err != nil {
			return model.CanvasAgentRun{}, errors.New("Skill 不存在或尚未发布")
		}
	}
	if len(input.CanvasContext) > 512<<10 {
		return model.CanvasAgentRun{}, errors.New("画布上下文过大，请只提交选中节点和关联节点")
	}
	tools := filterCanvasAgentTools(canvasAgentCanonicalTools(), skill.AllowedTools, input.SkillID != "")
	toolsJSON, _ := json.Marshal(tools)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	runToken := randomRunToken()
	run := model.CanvasAgentRun{ID: uuid.NewString(), Token: runToken, TokenHash: hashCanvasAgentToken(runToken), SessionID: strings.TrimSpace(input.SessionID), CanvasID: strings.TrimSpace(input.CanvasID), SkillID: strings.TrimSpace(input.SkillID), Model: modelName, ChannelID: strings.TrimSpace(input.ChannelID), UserChannelID: strings.TrimSpace(input.UserChannelID), Status: model.CanvasAgentRunStatusRunning, Phase: strings.TrimSpace(input.Phase), MaxSteps: canvasAgentMaxSteps, Input: input.Input, Tools: toolsJSON, CreatedAt: now, UpdatedAt: now}
	if len(input.CanvasContext) > 0 {
		run.Input = input.CanvasContext
	}
	user, ok := UserFromContext(ctx)
	if !ok {
		return model.CanvasAgentRun{}, errors.New("请先登录")
	}
	run.OwnerID = user.ID
	unlockSession := lockCanvasAgentRun("session:" + run.OwnerID + ":" + run.CanvasID + ":" + run.SessionID)
	defer unlockSession()
	protocol := []map[string]any{}
	if previous, found, findErr := repository.FindLatestCanvasAgentRun(run.SessionID, run.CanvasID, run.OwnerID); findErr != nil {
		return model.CanvasAgentRun{}, findErr
	} else if found {
		if previous.Status == model.CanvasAgentRunStatusRunning || previous.Status == model.CanvasAgentRunStatusWaiting {
			return model.CanvasAgentRun{}, errors.New("当前会话已有未完成的 Agent Run，请先恢复或取消")
		}
		if previous.Phase != "" {
			run.Phase = previous.Phase
		}
		_ = json.Unmarshal(previous.Protocol, &protocol)
	}
	protocol = append(protocol, map[string]any{"role": "user", "content": strings.TrimSpace(input.Message)})
	run.Protocol, _ = json.Marshal(protocol)
	data := marshalJSON(map[string]any{"status": run.Status, "phase": run.Phase})
	if err := repository.CreateCanvasAgentRun(run, model.CanvasAgentEvent{RunID: run.ID, Type: model.CanvasAgentEventRunCreated, Data: data, CreatedAt: now}); err != nil {
		return run, err
	}
	go continueCanvasAgentRun(run.ID)
	return run, nil
}

func RecoverCanvasAgentRun(ctx context.Context, sessionID, canvasID string) (model.CanvasAgentRun, error) {
	user, ok := UserFromContext(ctx)
	if !ok || user.ID == "" {
		return model.CanvasAgentRun{}, errors.New("请先登录")
	}
	ownerID := user.ID
	run, found, err := repository.FindLatestCanvasAgentRun(strings.TrimSpace(sessionID), strings.TrimSpace(canvasID), ownerID)
	if err != nil {
		return run, err
	}
	if !found || (run.Status != model.CanvasAgentRunStatusRunning && run.Status != model.CanvasAgentRunStatusWaiting) {
		return run, errors.New("当前会话没有可恢复的 Agent Run")
	}
	token := randomRunToken()
	if err := repository.UpdateCanvasAgentRun(run.ID, map[string]any{"token_hash": hashCanvasAgentToken(token), "updated_at": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return run, err
	}
	run.Token = token
	return run, nil
}

func SubmitCanvasAgentToolResults(ctx context.Context, id, token string, inputs []CanvasAgentToolResultInput) (model.CanvasAgentRun, error) {
	if len(inputs) == 0 {
		return model.CanvasAgentRun{}, errors.New("工具结果不能为空")
	}
	run, err := authorizedCanvasAgentRun(ctx, id, token)
	if err != nil {
		return run, err
	}
	unlock := lockCanvasAgentRun(id)
	defer unlock()
	run, _ = repository.FindCanvasAgentRun(id)
	if run.Status != model.CanvasAgentRunStatusWaiting {
		return run, errors.New("Run 当前不等待工具结果")
	}
	var pending []model.CanvasAgentToolCall
	if json.Unmarshal(run.PendingToolCalls, &pending) != nil || len(pending) == 0 {
		return run, errors.New("Run 没有有效的待处理工具调用")
	}
	provided := map[string]CanvasAgentToolResultInput{}
	for _, input := range inputs {
		provided[strings.TrimSpace(input.CallID)] = input
	}
	var protocol []map[string]any
	_ = json.Unmarshal(run.Protocol, &protocol)
	nextPhase := ""
	for _, call := range pending {
		result, ok := provided[call.ID]
		if !ok {
			return run, fmt.Errorf("缺少工具 %s 的结果", call.ID)
		}
		content := string(result.Result)
		if content == "" {
			content = marshalJSONString(map[string]any{"ok": result.Error == "", "error": result.Error})
		}
		protocol = append(protocol, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": content})
		if call.Name == "set_agent_state" {
			var args struct {
				Phase string `json:"phase"`
			}
			if json.Unmarshal(call.Arguments, &args) == nil {
				nextPhase = strings.TrimSpace(args.Phase)
			}
		}
	}
	protocolJSON, _ := json.Marshal(protocol)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := model.CanvasAgentEvent{RunID: id, Type: model.CanvasAgentEventToolAccepted, Data: marshalJSON(map[string]any{"callIds": keys(provided)}), CreatedAt: now}
	if err := repository.AcceptCanvasAgentToolResults(id, pending, protocolJSON, nextPhase, event, now); err != nil {
		return run, err
	}
	run.Status, run.Protocol, run.PendingToolCalls, run.UpdatedAt = model.CanvasAgentRunStatusRunning, protocolJSON, nil, now
	if nextPhase != "" {
		run.Phase = nextPhase
	}
	go continueCanvasAgentRun(id)
	return run, nil
}

func CancelCanvasAgentRun(ctx context.Context, id, token string) (model.CanvasAgentRun, error) {
	run, err := authorizedCanvasAgentRun(ctx, id, token)
	if err != nil {
		return run, err
	}
	if value, ok := canvasAgentRunCancels.Load(id); ok {
		value.(context.CancelFunc)()
	}
	for attempt := 0; attempt < 2; attempt++ {
		if isCanvasAgentTerminal(run.Status) {
			return run, nil
		}
		if run.Status == model.CanvasAgentRunStatusWaiting {
			run = closePendingCanvasAgentCalls(run, "Agent 已取消，工具未执行")
		}
		cancelled, finishErr := finishCanvasAgentRun(run, model.CanvasAgentRunStatusCancelled, "Agent 运行已取消", nil)
		if finishErr == nil {
			return cancelled, nil
		}
		run, err = repository.FindCanvasAgentRun(id)
		if err != nil {
			return run, err
		}
	}
	return run, errors.New("Agent Run 状态变化过快，请重试取消")
}

func CanvasAgentRunEvents(ctx context.Context, id, token string, after uint64) (model.CanvasAgentRun, []model.CanvasAgentEvent, error) {
	run, err := authorizedCanvasAgentRun(ctx, id, token)
	if err != nil {
		return run, nil, err
	}
	events, err := repository.ListCanvasAgentEvents(id, after)
	return run, events, err
}

func ResumeCanvasAgentRun(ctx context.Context, id, token string) error {
	run, err := authorizedCanvasAgentRun(ctx, id, token)
	if err != nil {
		return err
	}
	if run.Status == model.CanvasAgentRunStatusRunning {
		go continueCanvasAgentRun(id)
	}
	return nil
}

func continueCanvasAgentRun(id string) {
	unlock := lockCanvasAgentRun(id)
	defer unlock()
	runContext, cancel := context.WithCancel(context.Background())
	canvasAgentRunCancels.Store(id, cancel)
	defer func() { cancel(); canvasAgentRunCancels.Delete(id) }()
	run, err := repository.FindCanvasAgentRun(id)
	if err != nil || run.Status != model.CanvasAgentRunStatusRunning {
		return
	}
	for run.Status == model.CanvasAgentRunStatusRunning {
		if run.Step >= run.MaxSteps {
			_, _ = finishCanvasAgentRun(run, model.CanvasAgentRunStatusFailed, "Agent 超过最大执行步数", nil)
			return
		}
		latest, _ := repository.FindCanvasAgentRun(id)
		if latest.Status == model.CanvasAgentRunStatusCancelled {
			return
		}
		run = latest
		_ = saveCanvasAgentEvent(id, model.CanvasAgentEventStatus, map[string]any{"status": model.CanvasAgentRunStatusRunning, "step": run.Step + 1})
		var content string
		var calls []model.CanvasAgentToolCall
		var streamed bool
		var callErr error
		for attempt := 1; attempt <= canvasAgentMaxCallAttempts; attempt++ {
			content, calls, streamed, callErr = requestCanvasAgentModelTurn(runContext, run)
			if callErr == nil || streamed || !isRetryableCanvasAgentCallError(callErr) || attempt == canvasAgentMaxCallAttempts {
				break
			}
			_ = saveCanvasAgentEvent(id, model.CanvasAgentEventStatus, map[string]any{
				"status": model.CanvasAgentRunStatusRunning,
				"step": run.Step + 1,
				"retryAttempt": attempt,
				"maxAttempts": canvasAgentMaxCallAttempts,
			})
			timer := time.NewTimer(time.Duration(attempt) * time.Second)
			select {
			case <-runContext.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if callErr != nil {
			latest, _ = repository.FindCanvasAgentRun(id)
			if latest.Status != model.CanvasAgentRunStatusCancelled {
				_, _ = finishCanvasAgentRun(run, model.CanvasAgentRunStatusFailed, callErr.Error(), nil)
			}
			return
		}
		latest, _ = repository.FindCanvasAgentRun(id)
		if latest.Status == model.CanvasAgentRunStatusCancelled {
			return
		}
		run.Step++
		if content != "" {
			if !streamed {
				_ = saveCanvasAgentEvent(id, model.CanvasAgentEventTextDelta, map[string]any{"delta": content, "step": run.Step})
			}
			_ = saveCanvasAgentEvent(id, model.CanvasAgentEventTextCompleted, map[string]any{"text": content, "step": run.Step})
		}
		var protocol []map[string]any
		_ = json.Unmarshal(run.Protocol, &protocol)
		assistant := map[string]any{"role": "assistant", "content": content}
		if len(calls) > 0 {
			delete(assistant, "content")
			toolCalls := make([]map[string]any, 0, len(calls))
			for _, call := range calls {
				toolCalls = append(toolCalls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			assistant["tool_calls"] = toolCalls
		}
		protocol = append(protocol, assistant)
		protocolJSON, _ := json.Marshal(protocol)
		if len(calls) == 0 {
			run.Protocol, run.Output = protocolJSON, marshalJSON(map[string]any{"text": content})
			_, _ = finishCanvasAgentRun(run, model.CanvasAgentRunStatusCompleted, "", run.Output)
			return
		}
		pending, _ := json.Marshal(calls)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		event := model.CanvasAgentEvent{RunID: id, Type: model.CanvasAgentEventToolRequested, Data: marshalJSON(map[string]any{"calls": calls, "step": run.Step}), CreatedAt: now}
		_ = repository.UpdateCanvasAgentRunWithEvent(id, model.CanvasAgentRunStatusRunning, map[string]any{"status": model.CanvasAgentRunStatusWaiting, "step": run.Step, "protocol": protocolJSON, "pending_tool_calls": pending, "updated_at": now}, event)
		return
	}
}

func requestCanvasAgentModelTurn(ctx context.Context, run model.CanvasAgentRun) (string, []model.CanvasAgentToolCall, bool, error) {
	channel, err := canvasAgentChannel(run)
	if err != nil {
		return "", nil, false, err
	}
	credits := 0
	succeeded := false
	if run.OwnerID != "" && run.UserChannelID == "" && channel.ID != "client" {
		credits, err = ModelCost(run.Model)
		if err != nil {
			return "", nil, false, err
		}
		if credits > 0 {
			if err := ConsumeUserCredits(run.OwnerID, run.Model, credits, "/canvas-agent/runs"); err != nil {
				return "", nil, false, err
			}
		}
	}
	defer func() {
		if !succeeded && credits > 0 {
			_ = RefundUserCredits(run.OwnerID, run.Model, credits, "/canvas-agent/runs")
		}
	}()
	var messages []map[string]any
	_ = json.Unmarshal(run.Protocol, &messages)
	for _, message := range messages {
		if message["role"] == "tool" {
			delete(message, "name")
		}
		if message["role"] == "assistant" && message["tool_calls"] != nil && message["content"] == "" {
			delete(message, "content")
		}
	}
	system, err := canvasAgentSystemPrompt(run)
	if err != nil {
		return "", nil, false, err
	}
	messages = append([]map[string]any{{"role": "system", "content": system}}, messages...)
	body := map[string]any{"model": run.Model, "messages": messages, "stream": true}
	var tools []map[string]any
	_ = json.Unmarshal(run.Tools, &tools)
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		parameters, _ := function["parameters"].(map[string]any)
		if parameters != nil && parameters["properties"] == nil {
			parameters["properties"] = map[string]any{}
		}
	}
	if len(tools) > 0 {
		body["tools"], body["tool_choice"] = tools, "auto"
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildModelChannelURL(channel, "/chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return "", nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+channel.APIKey)
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := HTTPClientForChannel(channel).Do(req)
	if err != nil {
		saveCanvasAgentCallLog(run, channel, payload, nil, 0, started, err)
		return "", nil, false, fmt.Errorf("上游模型请求失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		upstream := readChannelError(string(responseBody), "上游模型请求失败")
		saveCanvasAgentCallLog(run, channel, payload, responseBody, response.StatusCode, started, upstream)
		return "", nil, false, upstream
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		content, calls, responseBody, streamErr := readCanvasAgentModelStream(run.ID, response.Body)
		if streamErr != nil {
			saveCanvasAgentCallLog(run, channel, payload, responseBody, response.StatusCode, started, streamErr)
			return "", nil, true, streamErr
		}
		allowed := canvasAgentToolNames(tools)
		for _, call := range calls {
			if _, ok := allowed[call.Name]; !ok {
				return "", nil, true, fmt.Errorf("模型请求了未授权工具：%s", call.Name)
			}
		}
		saveCanvasAgentCallLog(run, channel, payload, responseBody, response.StatusCode, started, nil)
		succeeded = true
		return content, calls, true, nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode >= 400 {
		upstream := readChannelError(string(responseBody), "上游模型请求失败")
		saveCanvasAgentCallLog(run, channel, payload, responseBody, response.StatusCode, started, upstream)
		return "", nil, false, upstream
	}
	if rateLimitMessage := canvasAgentRateLimitError(responseBody); rateLimitMessage != "" {
		err := errors.New(rateLimitMessage)
		saveCanvasAgentCallLog(run, channel, payload, responseBody, http.StatusTooManyRequests, started, err)
		return "", nil, false, err
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		LimitReached bool `json:"limit_reached"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", nil, false, errors.New("上游模型返回了无法解析的响应")
	}
	if result.LimitReached {
		return "", nil, false, errors.New("当前模型渠道额度已用完，请更换模型或渠道")
	}
	if result.Error != nil {
		return "", nil, false, errors.New(result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", nil, false, errors.New("上游模型没有返回有效结果")
	}
	message := result.Choices[0].Message
	calls := make([]model.CanvasAgentToolCall, 0, len(message.ToolCalls))
	allowed := canvasAgentToolNames(tools)
	for _, item := range message.ToolCalls {
		name := strings.TrimSpace(item.Function.Name)
		if _, ok := allowed[name]; !ok {
			return "", nil, false, fmt.Errorf("模型请求了未授权工具：%s", name)
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = uuid.NewString()
		}
		arguments := item.Function.Arguments
		if len(arguments) > 1 && arguments[0] == '"' {
			var encoded string
			if json.Unmarshal(arguments, &encoded) == nil {
				arguments = json.RawMessage(encoded)
			}
		}
		if len(arguments) == 0 || !json.Valid(arguments) {
			arguments = json.RawMessage(`{}`)
		}
		calls = append(calls, model.CanvasAgentToolCall{ID: id, Name: name, Arguments: arguments})
	}
	saveCanvasAgentCallLog(run, channel, payload, responseBody, response.StatusCode, started, nil)
	succeeded = true
	return strings.TrimSpace(message.Content), calls, false, nil
}

func readCanvasAgentModelStream(runID string, body io.Reader) (string, []model.CanvasAgentToolCall, []byte, error) {
	type partialCall struct{ ID, Name, Arguments string }
	scanner := bufio.NewScanner(io.LimitReader(body, 8<<20))
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var raw, content strings.Builder
	calls := map[int]*partialCall{}
	consume := func(data string) error {
		if data == "" || data == "[DONE]" {
			return nil
		}
		raw.WriteString("data: ")
		raw.WriteString(data)
		raw.WriteString("\n\n")
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			RateLimits struct {
				Allowed      *bool `json:"allowed"`
				LimitReached bool  `json:"limit_reached"`
			} `json:"rate_limits"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int                              `json:"index"`
						ID       string                           `json:"id"`
						Function struct{ Name, Arguments string } `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Item *struct {
				Type      string          `json:"type"`
				ID        string          `json:"id"`
				CallID    string          `json:"call_id"`
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil
		}
		if event.Error != nil && event.Error.Message != "" {
			return errors.New(event.Error.Message)
		}
		if event.Type == "codex.rate_limits" && ((event.RateLimits.Allowed != nil && !*event.RateLimits.Allowed) || event.RateLimits.LimitReached) {
			return errors.New("当前模型渠道额度已用完，请更换模型或渠道")
		}
		appendDelta := func(delta string) {
			if delta != "" {
				content.WriteString(delta)
				_ = saveCanvasAgentEvent(runID, model.CanvasAgentEventTextDelta, map[string]any{"delta": delta})
			}
		}
		if event.Type == "response.output_text.delta" {
			appendDelta(event.Delta)
		}
		if event.Type == "response.output_item.done" && event.Item != nil && event.Item.Type == "function_call" {
			calls[len(calls)] = &partialCall{ID: firstNonEmpty(event.Item.CallID, event.Item.ID), Name: event.Item.Name, Arguments: string(event.Item.Arguments)}
		}
		for _, choice := range event.Choices {
			appendDelta(choice.Delta.Content)
			for _, call := range choice.Delta.ToolCalls {
				current := calls[call.Index]
				if current == nil {
					current = &partialCall{}
					calls[call.Index] = current
				}
				if call.ID != "" {
					current.ID = call.ID
				}
				if call.Function.Name != "" {
					current.Name = call.Function.Name
				}
				current.Arguments += call.Function.Arguments
			}
		}
		return nil
	}
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := consume(strings.Join(dataLines, "\n")); err != nil {
				return "", nil, []byte(raw.String()), err
			}
			dataLines = nil
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) > 0 {
		if err := consume(strings.Join(dataLines, "\n")); err != nil {
			return "", nil, []byte(raw.String()), err
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, []byte(raw.String()), err
	}
	result := make([]model.CanvasAgentToolCall, 0, len(calls))
	for index := 0; index < len(calls); index++ {
		call := calls[index]
		if call == nil || call.Name == "" {
			continue
		}
		arguments := json.RawMessage(call.Arguments)
		if len(arguments) > 1 && arguments[0] == '"' {
			var encoded string
			if json.Unmarshal(arguments, &encoded) == nil {
				arguments = json.RawMessage(encoded)
			}
		}
		if len(arguments) == 0 || !json.Valid(arguments) {
			arguments = json.RawMessage(`{}`)
		}
		if call.ID == "" {
			call.ID = uuid.NewString()
		}
		result = append(result, model.CanvasAgentToolCall{ID: call.ID, Name: call.Name, Arguments: arguments})
	}
	return strings.TrimSpace(content.String()), result, []byte(raw.String()), nil
}

func canvasAgentSystemPrompt(run model.CanvasAgentRun) (string, error) {
	base := "你是画布创作 Agent。根据用户目标和精简画布上下文自主思考；需要读取或修改画布时只能调用已提供工具。不得声称执行未成功的操作。"
	if settings, err := repository.GetSettings(); err == nil {
		if value := strings.TrimSpace(normalizeSettings(settings).Public.ModelChannel.SystemPrompts.Text); value != "" {
			base = value + "\n\n" + base
		}
	}
	if run.SkillID != "" {
		skill, err := repository.FindCanvasSkill(run.SkillID, true)
		if err != nil {
			return "", errors.New("运行中的 Skill 已不可用")
		}
		base += "\n\n# Skill: " + skill.Name + "\n" + skill.Instructions
		for name, content := range skill.Files {
			base += "\n\n## 参考文件: " + name + "\n" + content
		}
	}
	if len(run.Input) > 0 {
		base += "\n\n# 当前画布上下文\n" + string(run.Input)
	}
	return base, nil
}

func canvasAgentChannel(run model.CanvasAgentRun) (model.ModelChannel, error) {
	if run.UserChannelID != "" {
		return SelectUserLocalModelChannelForModel(run.OwnerID, run.Model, run.UserChannelID)
	}
	if run.OwnerID != "" {
		user, ok, err := repository.GetUserByID(run.OwnerID)
		if err == nil && ok && !UserCanUseRemoteModelChannel(model.PublicUser(user)) {
			return model.ModelChannel{}, errors.New("当前账号未开放云端渠道")
		}
	}
	return SelectModelChannelForModel(run.Model, run.ChannelID)
}

func filterCanvasAgentTools(tools []map[string]any, allowed []string, enforce bool) []map[string]any {
	allow := map[string]bool{}
	for _, name := range allowed {
		allow[name] = true
	}
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		if name != "" && (!enforce || allow[name]) {
			result = append(result, tool)
		}
	}
	return result
}

func canvasAgentCanonicalTools() []map[string]any {
	stringValue := map[string]any{"type": "string"}
	stringArray := map[string]any{"type": "array", "items": stringValue, "maxItems": 50}
	number := map[string]any{"type": "number"}
	tool := func(name, description string, properties map[string]any, required ...string) map[string]any {
		if properties == nil {
			properties = map[string]any{}
		}
		parameters := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			parameters["required"] = required
		}
		return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": parameters}}
	}
	node := func(name, description string) map[string]any {
		return tool(name, description, map[string]any{"nodeId": stringValue}, "nodeId")
	}
	return []map[string]any{
		tool("get_canvas_summary", "读取当前精简画布摘要。", nil), tool("get_selected_nodes", "读取用户选中的节点。", nil),
		node("get_node", "按节点 ID 读取节点。"), node("get_upstream_nodes", "读取直接上游节点。"), node("get_downstream_nodes", "读取直接下游节点。"), node("get_connected_nodes", "读取直接连接节点。"),
		tool("get_generation_config", "读取当前生成配置。", nil), node("get_generation_task", "读取媒体生成任务。"), node("get_media_task_status", "读取媒体任务状态。"),
		tool("set_agent_state", "保存创作阶段和已确认内容。", map[string]any{"phase": map[string]any{"type": "string", "enum": []string{"intake", "concept", "script", "breakdown", "references", "storyboard", "video", "audio", "review", "complete"}}, "brief": stringValue, "targetDurationSeconds": number, "approvedPlan": stringValue, "approvedNodeIds": stringArray, "referenceNodeIds": stringArray}, "phase"),
		tool("create_primary_script_node", "创建正式主剧本节点。", map[string]any{"title": stringValue, "content": stringValue, "sourceNodeIds": stringArray, "projectTitle": stringValue}, "title", "content", "projectTitle"),
		tool("create_text_node", "创建普通文本节点。", map[string]any{"title": stringValue, "content": stringValue, "sourceNodeIds": stringArray}, "title", "content"),
		tool("update_text_node", "更新文本节点。", map[string]any{"nodeId": stringValue, "title": stringValue, "content": stringValue}, "nodeId"),
		tool("update_node", "更新节点标题。", map[string]any{"nodeId": stringValue, "title": stringValue}, "nodeId", "title"),
		node("delete_node", "删除节点。"),
		tool("create_connection", "创建来源连线。", map[string]any{"fromNodeId": stringValue, "toNodeId": stringValue}, "fromNodeId", "toNodeId"),
		tool("delete_connection", "删除连线。", map[string]any{"connectionId": stringValue}, "connectionId"),
		tool("create_group", "把至少两个节点放入分组。", map[string]any{"title": stringValue, "nodeIds": stringArray}, "nodeIds"),
		tool("arrange_nodes", "整理节点布局。", map[string]any{"nodeIds": stringArray}),
		tool("generate_image", "生成图片。", map[string]any{"prompt": stringValue, "title": stringValue, "sourceNodeIds": stringArray, "size": stringValue, "count": number}, "prompt", "sourceNodeIds"),
		tool("edit_image", "编辑已有图片。", map[string]any{"prompt": stringValue, "title": stringValue, "sourceNodeIds": stringArray, "size": stringValue, "count": number}, "prompt", "sourceNodeIds"),
		tool("generate_video", "生成视频，是否生成原生音频由用户的全局视频配置决定。", map[string]any{"prompt": stringValue, "title": stringValue, "sourceNodeIds": stringArray, "size": stringValue, "seconds": number}, "prompt", "sourceNodeIds"),
		tool("generate_audio", "生成独立音频。", map[string]any{"prompt": stringValue, "title": stringValue, "sourceNodeIds": stringArray, "voice": stringValue, "instructions": stringValue}, "prompt", "sourceNodeIds"),
	}
}

func canvasAgentToolNames(tools []map[string]any) map[string]struct{} {
	result := map[string]struct{}{}
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		if name, _ := function["name"].(string); name != "" {
			result[name] = struct{}{}
		}
	}
	return result
}

func finishCanvasAgentRun(run model.CanvasAgentRun, status, errorText string, output json.RawMessage) (model.CanvasAgentRun, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	values := map[string]any{"status": status, "step": run.Step, "protocol": run.Protocol, "pending_tool_calls": nil, "output": output, "error": errorText, "updated_at": now}
	eventType := model.CanvasAgentEventCompleted
	if status == model.CanvasAgentRunStatusFailed {
		eventType = model.CanvasAgentEventFailed
	}
	if status == model.CanvasAgentRunStatusCancelled {
		eventType = model.CanvasAgentEventCancelled
	}
	event := model.CanvasAgentEvent{RunID: run.ID, Type: eventType, Data: marshalJSON(map[string]any{"status": status, "phase": run.Phase, "error": errorText, "output": output}), CreatedAt: now}
	if err := repository.UpdateCanvasAgentRunWithEvent(run.ID, run.Status, values, event); err != nil {
		return run, err
	}
	run.Status, run.Error, run.Output, run.UpdatedAt = status, errorText, output, now
	return run, nil
}
func closePendingCanvasAgentCalls(run model.CanvasAgentRun, message string) model.CanvasAgentRun {
	var protocol []map[string]any
	var calls []model.CanvasAgentToolCall
	_ = json.Unmarshal(run.Protocol, &protocol)
	_ = json.Unmarshal(run.PendingToolCalls, &calls)
	for _, call := range calls {
		protocol = append(protocol, map[string]any{"role": "tool", "tool_call_id": call.ID, "content": marshalJSONString(map[string]any{"ok": false, "code": "cancelled", "message": message})})
	}
	run.Protocol, _ = json.Marshal(protocol)
	run.PendingToolCalls = nil
	return run
}

func saveCanvasAgentEvent(id, eventType string, data any) error {
	return repository.SaveCanvasAgentEvent(model.CanvasAgentEvent{RunID: id, Type: eventType, Data: marshalJSON(data), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}
func authorizedCanvasAgentRun(ctx context.Context, id, token string) (model.CanvasAgentRun, error) {
	run, err := repository.FindCanvasAgentRun(strings.TrimSpace(id))
	if err != nil || run.TokenHash == "" || run.TokenHash != hashCanvasAgentToken(strings.TrimSpace(token)) {
		return model.CanvasAgentRun{}, errors.New("Agent Run 不存在或访问凭证无效")
	}
	if run.OwnerID != "" {
		user, ok := UserFromContext(ctx)
		if !ok || user.ID != run.OwnerID {
			return model.CanvasAgentRun{}, errors.New("无权访问此 Agent Run")
		}
	}
	return run, nil
}
func hashCanvasAgentToken(token string) string {
	value := sha256.Sum256([]byte(token))
	return hex.EncodeToString(value[:])
}
func isCanvasAgentTerminal(status string) bool {
	return status == model.CanvasAgentRunStatusCompleted || status == model.CanvasAgentRunStatusFailed || status == model.CanvasAgentRunStatusCancelled
}
func lockCanvasAgentRun(id string) func() {
	value, _ := canvasAgentRunLocks.LoadOrStore(id, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}
func randomRunToken() string {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(value)
}
func marshalJSON(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
func marshalJSONString(value any) string    { return string(marshalJSON(value)) }
func keys(values map[string]CanvasAgentToolResultInput) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
func saveCanvasAgentCallLog(run model.CanvasAgentRun, channel model.ModelChannel, request, response []byte, status int, started time.Time, requestErr error) {
	errorText := ""
	if requestErr != nil {
		errorText = requestErr.Error()
	}
	SaveAICallLog(AICallLogInput{UserID: run.OwnerID, Endpoint: "/canvas-agent/runs", Method: http.MethodPost, Model: run.Model, ChannelID: channel.ID, ChannelName: channel.Name, Status: status, DurationMs: time.Since(started).Milliseconds(), RequestBody: string(request), ResponseBody: string(response), Error: errorText})
}
func isRetryableCanvasAgentCallError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "rate limit") || strings.Contains(message, "limit_reached") || strings.Contains(message, "quota") || strings.Contains(message, "401") || strings.Contains(message, "403") {
		return false
	}
	for _, marker := range []string{"temporarily unavailable", "bad gateway", "gateway timeout", "connection reset", "connection refused", "forcibly closed", "wsarecv", "broken pipe", "unexpected eof", " 502", " 503", " 504"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
func canvasAgentRateLimitError(body []byte) string {
	text := string(body)
	if !strings.Contains(text, "codex.rate_limits") && !strings.Contains(text, "limit_reached") {
		return ""
	}
	if strings.Contains(text, `"allowed":false`) || strings.Contains(text, `"allowed": false`) || strings.Contains(text, `"limit_reached":true`) || strings.Contains(text, `"limit_reached": true`) {
		return "当前模型渠道额度已用完，请更换模型或渠道"
	}
	return ""
}
