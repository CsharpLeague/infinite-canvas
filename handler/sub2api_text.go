package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
)

func isSub2APITextRequest(channel model.ModelChannel, path string) bool {
	return path == "/chat/completions" && strings.EqualFold(strings.TrimSpace(channel.Protocol), "sub2api")
}

func normalizeSub2APITextBody(body []byte, contentType string) ([]byte, string, error) {
	if !strings.Contains(strings.ToLower(contentType), "application/json") {
		return nil, "", fmt.Errorf("Sub2API 文本请求必须使用 JSON")
	}
	var input map[string]any
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, "", fmt.Errorf("解析 Sub2API 文本请求失败: %w", err)
	}
	messages, _ := input["messages"].([]any)
	responsesInput := make([]any, 0, len(messages))
	instructions := make([]string, 0, 1)
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(fmt.Sprint(message["role"]))
		if role == "system" {
			if text := responseMessageText(message["content"]); text != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		responsesInput = append(responsesInput, map[string]any{
			"role":    role,
			"content": normalizeResponsesMessageContent(message["content"]),
		})
	}
	payload := map[string]any{
		"model":  input["model"],
		"input":  responsesInput,
		"stream": input["stream"],
	}
	if len(instructions) > 0 {
		payload["instructions"] = strings.Join(instructions, "\n\n")
	}
	for _, key := range []string{"temperature", "max_output_tokens", "reasoning", "tools", "tool_choice"} {
		if value, ok := input[key]; ok {
			payload[key] = value
		}
	}
	result, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("生成 Sub2API Responses 请求失败: %w", err)
	}
	return result, "application/json", nil
}

func normalizeResponsesMessageContent(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	content := make([]any, 0, len(items))
	for _, item := range items {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch part["type"] {
		case "text":
			content = append(content, map[string]any{"type": "input_text", "text": part["text"]})
		case "image_url":
			image, _ := part["image_url"].(map[string]any)
			content = append(content, map[string]any{"type": "input_image", "image_url": image["url"]})
		default:
			content = append(content, part)
		}
	}
	return content
}

func responseMessageText(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	items, _ := value.([]any)
	texts := make([]string, 0, len(items))
	for _, item := range items {
		part, _ := item.(map[string]any)
		if part["type"] == "text" {
			if text := strings.TrimSpace(fmt.Sprint(part["text"])); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}
