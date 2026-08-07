package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func StartVideoTaskPoller() {
	service.StartVideoTaskPoller(pollVideoTaskFromUpstream)
}

func UserVideoTasks(w http.ResponseWriter, r *http.Request) {
	user, ok := service.UserFromContext(r.Context())
	if !ok {
		Fail(w, "未登录或权限不足")
		return
	}
	tasks, err := service.ListUserVideoTasks(user.ID, "video-workbench", 100)
	if err != nil {
		log.Printf("list video tasks failed: user=%s err=%v", user.ID, err)
		Fail(w, "AI 接口请求失败")
		return
	}
	OK(w, tasks)
}

func DeleteUserVideoTask(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := service.UserFromContext(r.Context())
	if !ok {
		Fail(w, "未登录或权限不足")
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		Fail(w, "视频任务不存在")
		return
	}
	if err := service.DeleteUserVideoTask(user.ID, id); err != nil {
		log.Printf("delete video task failed: user=%s id=%s err=%v", user.ID, id, err)
		Fail(w, "AI 接口请求失败")
		return
	}
	OK(w, map[string]any{"deleted": true})
}

func proxyAIVideoTaskRequest(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	body, contentType, modelName, err := readAIRequest(r)
	if err != nil {
		log.Printf("AI video request read failed: %v", err)
		Fail(w, "AI 接口请求失败")
		return
	}
	user, ok := service.UserFromContext(r.Context())
	if !ok {
		Fail(w, "未登录或权限不足")
		return
	}
	channel, userChannelID, err := selectAIRequestChannel(user, modelName, r.Header.Get("X-Model-Channel-ID"), r.Header.Get(userModelChannelHeader))
	if err != nil {
		log.Printf("AI video select channel failed: model=%s err=%v", modelName, err)
		failAIChannelSelect(w, err, "AI 接口请求失败")
		return
	}
	credits := 0
	if userChannelID == "" {
		resolution, seconds := readVideoBillingParameters(body, contentType)
		credits, err = service.VideoModelCost(modelName, resolution, seconds)
		if err != nil {
			log.Printf("AI video read model cost failed: model=%s err=%v", modelName, err)
			Fail(w, "AI 接口请求失败")
			return
		}
	}
	upstreamPath := resolveAIProxyPath(channel, modelName, "/videos")
	body, contentType, err = normalizeVideoCreateBody(body, contentType, modelName, channel, upstreamPath)
	if err != nil {
		log.Printf("AI video normalize request failed: model=%s err=%v", modelName, err)
		Fail(w, err.Error())
		return
	}
	request, err := http.NewRequest(http.MethodPost, service.BuildModelChannelURL(channel, upstreamPath), bytes.NewReader(body))
	if err != nil {
		log.Printf("AI video build request failed: url=%s err=%v", service.BuildModelChannelURL(channel, upstreamPath), err)
		Fail(w, "AI 接口请求失败")
		return
	}
	request.Header.Set("Authorization", "Bearer "+channel.APIKey)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	logContext := aiLogContext{
		StartedAt:       startedAt,
		Endpoint:        "/videos",
		Method:          http.MethodPost,
		Model:           modelName,
		Channel:         channel,
		UserID:          user.ID,
		UserDisplayName: firstNonEmpty(user.DisplayName, user.Username),
		Credits:         credits,
		RequestBody:     summarizeAIRequest(body, contentType),
	}
	clientTaskID := readClientVideoTaskID(r)
	taskSource := readVideoTaskSource(r)
	taskSourceID := readVideoTaskSourceID(r)
	if credits > 0 {
		if err := service.ConsumeUserCredits(user.ID, modelName, credits, upstreamPath); err != nil {
			FailError(w, err)
			return
		}
	}
	var pendingTask *model.VideoTask
	if clientTaskID != "" {
		task, saveErr := service.CreateVideoTask(service.VideoTaskCreateInput{
			UserID: user.ID, UserDisplayName: firstNonEmpty(user.DisplayName, user.Username), Model: modelName,
			ChannelID: channel.ID, UserChannelID: userChannelID, ChannelName: channel.Name,
			Source: taskSource, SourceID: taskSourceID, ClientTaskID: clientTaskID,
			Status: "queued", RequestBody: logContext.RequestBody, Credits: credits,
		})
		if saveErr != nil {
			log.Printf("save pending video task failed: model=%s err=%v", modelName, saveErr)
			if credits > 0 {
				refundVideoCredits(user.ID, modelName, credits, upstreamPath)
			}
			Fail(w, "AI 接口请求失败")
			return
		}
		pendingTask = &task
	}
	payload, status, err := doAIRequest(request, channel)
	if err != nil {
		if credits > 0 {
			refundVideoCredits(user.ID, modelName, credits, upstreamPath)
		}
		failPendingVideoTask(pendingTask, "视频任务创建失败", err.Error())
		saveAIProxyLog(logContext, 0, "", err.Error())
		Fail(w, "AI 接口请求失败")
		return
	}
	if status >= http.StatusBadRequest {
		message := readUpstreamAIErrorMessage(payload, status)
		if credits > 0 {
			refundVideoCredits(user.ID, modelName, credits, upstreamPath)
		}
		failPendingVideoTask(pendingTask, message, string(payload))
		saveAIProxyLog(logContext, status, string(payload), strings.TrimSpace(string(payload)))
		Fail(w, message)
		return
	}
	transformed := transformVideoCreatePayload(payload, request, channel, modelName)
	if message := readVideoCreateErrorMessage(payload, transformed, channel, modelName); message != "" {
		if credits > 0 {
			refundVideoCredits(user.ID, modelName, credits, upstreamPath)
		}
		failPendingVideoTask(pendingTask, message, string(transformed))
		saveAIProxyLog(logContext, status, string(payload), message)
		Fail(w, message)
		return
	}
	parsed := parseVideoTaskPayload(transformed, modelName)
	if parsed.UpstreamTaskID == "" && parsed.UpstreamVideoID == "" {
		if credits > 0 {
			refundVideoCredits(user.ID, modelName, credits, upstreamPath)
		}
		failPendingVideoTask(pendingTask, "视频接口没有返回任务 ID", string(transformed))
		saveAIProxyLog(logContext, status, string(transformed), "视频接口没有返回任务 ID")
		Fail(w, "视频接口没有返回任务 ID")
		return
	}
	task, err := service.CreateVideoTask(service.VideoTaskCreateInput{
		UserID:          user.ID,
		UserDisplayName: firstNonEmpty(user.DisplayName, user.Username),
		Model:           modelName,
		ChannelID:       channel.ID,
		UserChannelID:   userChannelID,
		ChannelName:     channel.Name,
		Source:          taskSource,
		SourceID:        taskSourceID,
		ClientTaskID:    clientTaskID,
		UpstreamTaskID:  parsed.UpstreamTaskID,
		UpstreamVideoID: parsed.UpstreamVideoID,
		Status:          parsed.Status,
		Progress:        parsed.Progress,
		Seconds:         parsed.Seconds,
		Size:            parsed.Size,
		VideoURL:        parsed.VideoURL,
		FirstFrameURL:   parsed.FirstFrameURL,
		LastFrameURL:    parsed.LastFrameURL,
		Error:           parsed.Error,
		ErrorDetail:     parsed.ErrorDetail,
		RequestBody:     logContext.RequestBody,
		ResponseBody:    string(transformed),
		Credits:         credits,
	})
	if err != nil {
		log.Printf("save video task failed: model=%s err=%v", modelName, err)
		failPendingVideoTask(pendingTask, "视频任务保存失败", err.Error())
		Fail(w, "AI 接口请求失败")
		return
	}
	saveAIProxyLog(logContext, status, string(transformed), "")
	OK(w, service.VideoTaskResponse(task))
}

func failPendingVideoTask(task *model.VideoTask, message string, detail string) {
	if task == nil {
		return
	}
	if err := service.UpdateVideoTaskFromPoll(*task, service.VideoTaskPollUpdate{Status: "failed", Error: message, ErrorDetail: detail}); err != nil {
		log.Printf("mark pending video task failed: id=%s err=%v", task.ID, err)
	}
}

func readClientVideoTaskID(r *http.Request) string {
	id := strings.TrimSpace(r.Header.Get("X-Client-Video-Task-ID"))
	if isClientVideoTaskID(id) {
		return id
	}
	return ""
}

func readVideoTaskSource(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Video-Task-Source"))
}

func readVideoTaskSourceID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Video-Task-Source-ID"))
}

func isClientVideoTaskID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "client_video_task_")
}

func serveAIVideoTask(w http.ResponseWriter, r *http.Request, id string) bool {
	user, ok := service.UserFromContext(r.Context())
	if !ok {
		return false
	}
	task, found, err := service.GetUserVideoTask(user.ID, id)
	if err != nil {
		log.Printf("read video task failed: id=%s user=%s err=%v", id, user.ID, err)
		Fail(w, "AI 接口请求失败")
		return true
	}
	if !found {
		return false
	}
	if service.IsFailedVideoTaskStatus(task.Status) && strings.Contains(task.Error, "上传云存储") {
		if update, pollErr := pollVideoTaskFromUpstream(task); pollErr == nil {
			_ = service.UpdateVideoTaskFromPoll(task, update)
			task, _, _ = service.GetUserVideoTask(user.ID, id)
		}
	}
	OK(w, service.VideoTaskResponse(task))
	return true
}

func pollVideoTaskFromUpstream(task model.VideoTask) (service.VideoTaskPollUpdate, error) {
	var channel model.ModelChannel
	var err error
	if strings.TrimSpace(task.UserChannelID) != "" {
		channel, err = service.SelectUserLocalModelChannelForModel(task.UserID, task.Model, task.UserChannelID)
	} else {
		channel, err = service.SelectModelChannelForModel(task.Model, task.ChannelID)
	}
	if err != nil {
		return service.VideoTaskPollUpdate{}, err
	}
	pollID := firstNonEmpty(task.UpstreamTaskID, task.ID)
	if isAgnesVideoModel(task.Model) && strings.HasPrefix(task.UpstreamVideoID, "video_") {
		pollID = task.UpstreamVideoID
	}
	if strings.TrimSpace(pollID) == "" {
		return service.VideoTaskPollUpdate{}, errors.New("视频任务缺少上游任务 ID")
	}
	endpoint := "/videos/" + pollID
	upstreamPath := resolveAIProxyPath(channel, task.Model, endpoint)
	request, err := http.NewRequest(http.MethodGet, resolveAIProxyURL(channel, task.Model, upstreamPath), nil)
	if err != nil {
		return service.VideoTaskPollUpdate{}, err
	}
	request.Header.Set("Authorization", "Bearer "+channel.APIKey)
	startedAt := time.Now()
	logContext := aiLogContext{
		StartedAt:       startedAt,
		Endpoint:        endpoint,
		Method:          http.MethodGet,
		Model:           task.Model,
		Channel:         channel,
		UserID:          task.UserID,
		UserDisplayName: task.UserDisplayName,
		RequestBody:     fmt.Sprintf(`{"taskId":%q}`, pollID),
	}
	payload, status, err := doAIRequest(request, channel)
	if err != nil {
		saveAIProxyLog(logContext, 0, "", err.Error())
		return service.VideoTaskPollUpdate{}, err
	}
	if status >= http.StatusBadRequest {
		message := readUpstreamAIErrorMessage(payload, status)
		saveAIProxyLog(logContext, status, string(payload), strings.TrimSpace(string(payload)))
		if status == http.StatusTooManyRequests {
			return service.VideoTaskPollUpdate{Status: task.Status, ErrorDetail: message, ResponseBody: string(payload)}, nil
		}
		return service.VideoTaskPollUpdate{Status: "failed", Error: message, ErrorDetail: message, ResponseBody: string(payload)}, nil
	}
	transformed := transformVideoStatusPayload(payload, request, channel, task.Model)
	parsed := parseVideoTaskPayload(transformed, task.Model)
	if parsed.Status == "failed" && parsed.Error == "" {
		parsed.Error = firstNonEmpty(parsed.ErrorDetail, "视频任务生成失败")
	}
	if errMessage := readVideoStatusErrorMessage(payload, transformed, channel, task.Model); errMessage != "" {
		if parsed.Error == "" {
			parsed.Error = errMessage
		}
		parsed.Status = "failed"
	}
	if parsed.ErrorDetail == "" && len(payload) > 0 && parsed.Error != "" {
		parsed.ErrorDetail = string(payload)
	}
	saveAIProxyLog(logContext, status, string(transformed), firstNonEmpty(parsed.Error, ""))
	return service.VideoTaskPollUpdate{
		Status:        parsed.Status,
		Progress:      parsed.Progress,
		Seconds:       parsed.Seconds,
		Size:          parsed.Size,
		VideoURL:      parsed.VideoURL,
		FirstFrameURL: parsed.FirstFrameURL,
		LastFrameURL:  parsed.LastFrameURL,
		Error:         parsed.Error,
		ErrorDetail:   parsed.ErrorDetail,
		ResponseBody:  string(transformed),
	}, nil
}

func normalizeVideoCreateBody(body []byte, contentType string, modelName string, channel model.ModelChannel, upstreamPath string) ([]byte, string, error) {
	if isMiniMaxVideoChannel(channel, modelName) && upstreamPath == "/v2/video_generation" {
		return normalizeMiniMaxVideoBody(body, contentType, modelName)
	}
	if isArkSeedanceVideo(channel, modelName) && upstreamPath == "/contents/generations/tasks" {
		return normalizeArkSeedanceVideoBody(body, contentType, modelName)
	}
	if isKIEChannel(channel, modelName) && upstreamPath == "/jobs/createTask" {
		return normalizeKIEVideoBody(body, contentType, modelName, channel)
	}
	if isAPIMartChannel(channel, modelName) && upstreamPath == "/videos/generations" {
		return normalizeAPIMartVideoBody(body, contentType, modelName, channel)
	}
	return body, contentType, nil
}

func doAIRequest(request *http.Request, channel model.ModelChannel) ([]byte, int, error) {
	response, err := service.HTTPClientForChannel(channel).Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	return payload, response.StatusCode, nil
}

func transformVideoCreatePayload(payload []byte, request *http.Request, channel model.ModelChannel, modelName string) []byte {
	if isMiniMaxVideoChannel(channel, modelName) && strings.Contains(request.URL.Path, "/v2/video_generation") {
		if transformed, ok := transformMiniMaxCreateVideoResponse(payload, modelName); ok {
			return transformed
		}
	}
	if isKIEChannel(channel, modelName) && strings.Contains(request.URL.Path, "/jobs/createTask") {
		if transformed, ok := transformKIECreateVideoResponse(payload, modelName); ok {
			return transformed
		}
	}
	if isAPIMartChannel(channel, modelName) && strings.Contains(request.URL.Path, "/videos/generations") {
		if transformed, ok := transformAPIMartCreateVideoResponse(payload, modelName); ok {
			return transformed
		}
	}
	return payload
}

func transformVideoStatusPayload(payload []byte, request *http.Request, channel model.ModelChannel, modelName string) []byte {
	if isMiniMaxVideoChannel(channel, modelName) && strings.Contains(request.URL.Path, "/v2/query/video_generation/") {
		if transformed, ok := transformMiniMaxVideoTaskResponse(payload); ok {
			return transformed
		}
	}
	if isKIEChannel(channel, modelName) && strings.Contains(request.URL.Path, "/jobs/recordInfo") {
		if transformed, ok := transformKIETaskResponse(payload, modelName); ok {
			return transformed
		}
	}
	if isAPIMartChannel(channel, modelName) && strings.Contains(request.URL.Path, "/tasks/") {
		if transformed, ok := transformAPIMartTaskResponse(payload, modelName); ok {
			return transformed
		}
	}
	return payload
}

func readVideoCreateErrorMessage(raw []byte, transformed []byte, channel model.ModelChannel, modelName string) string {
	if isKIEChannel(channel, modelName) {
		return firstNonEmpty(readKIECreateTaskErrorMessage(raw), readProviderPayloadError(raw), readNormalizedVideoError(transformed))
	}
	return firstNonEmpty(readProviderPayloadError(raw), readNormalizedVideoError(transformed))
}

func readVideoStatusErrorMessage(raw []byte, transformed []byte, channel model.ModelChannel, modelName string) string {
	if isKIEChannel(channel, modelName) {
		return firstNonEmpty(readKIERecordInfoErrorMessage(raw), readProviderPayloadError(raw), readNormalizedVideoError(transformed))
	}
	return firstNonEmpty(readProviderPayloadError(raw), readNormalizedVideoError(transformed))
}

type parsedVideoTaskPayload struct {
	UpstreamTaskID  string
	UpstreamVideoID string
	Status          string
	Progress        int
	Seconds         string
	Size            string
	VideoURL        string
	FirstFrameURL   string
	LastFrameURL    string
	Error           string
	ErrorDetail     string
}

func parseVideoTaskPayload(payload []byte, modelName string) parsedVideoTaskPayload {
	var root any
	if len(payload) == 0 || json.Unmarshal(payload, &root) != nil {
		return parsedVideoTaskPayload{Status: "processing"}
	}
	data := normalizeVideoPayloadMap(root)
	result := parsedVideoTaskPayload{
		UpstreamTaskID:  firstNonEmpty(readStringPath(data, "task_id"), readStringPath(data, "taskId"), readStringPath(data, "id")),
		UpstreamVideoID: firstNonEmpty(readStringPath(data, "video_id"), readStringPath(data, "videoId")),
		Status:          service.NormalizeVideoTaskStatus(firstNonEmpty(readStringPath(data, "status"), readStringPath(data, "state"))),
		Progress:        readIntPath(data, "progress"),
		Seconds:         firstNonEmpty(readStringPath(data, "seconds"), readStringPath(data, "duration")),
		Size:            firstNonEmpty(readStringPath(data, "size"), readSizeFromDimensions(data)),
		VideoURL:        firstNonEmpty(readStringPath(data, "video_url"), readStringPath(data, "content.video_url"), readStringPath(data, "url"), readStringPath(data, "remixed_from_video_id"), readStringPath(data, "output_url"), readStringPath(data, "download_url"), findFirstHTTPURL(data)),
		FirstFrameURL:   firstNonEmpty(readStringPath(data, "first_frame_url"), readStringPath(data, "firstFrameUrl"), readStringPath(data, "content.first_frame_url"), readStringPath(data, "content.firstFrameUrl"), readStringPath(data, "result.first_frame_url")),
		LastFrameURL:    firstNonEmpty(readStringPath(data, "last_frame_url"), readStringPath(data, "lastFrameUrl"), readStringPath(data, "tail_frame_url"), readStringPath(data, "end_frame_url"), readStringPath(data, "content.last_frame_url"), readStringPath(data, "content.lastFrameUrl"), readStringPath(data, "content.tail_frame_url"), readStringPath(data, "result.last_frame_url")),
		Error:           firstNonEmpty(readStringPath(data, "error.message"), readStringPath(data, "error")),
		ErrorDetail:     "",
	}
	if result.UpstreamTaskID == result.UpstreamVideoID && strings.HasPrefix(result.UpstreamVideoID, "video_") {
		result.UpstreamTaskID = ""
	}
	if result.Status == "" {
		result.Status = "processing"
	}
	if result.VideoURL != "" {
		result.Status = "completed"
		result.Progress = 100
	}
	if result.Status == "failed" && result.Error == "" {
		result.Error = firstNonEmpty(readStringPath(data, "message"), readStringPath(data, "msg"), "视频任务生成失败")
	}
	if result.UpstreamVideoID == "" && isAgnesVideoModel(modelName) && strings.HasPrefix(result.VideoURL, "video_") {
		result.UpstreamVideoID = result.VideoURL
	}
	if result.Error != "" {
		result.ErrorDetail = string(payload)
	}
	return result
}

func normalizeVideoPayloadMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if data, ok := typed["data"].(map[string]any); ok {
			for key, item := range typed {
				if _, exists := data[key]; !exists {
					data[key] = item
				}
			}
			return data
		}
		if data, ok := typed["data"].([]any); ok && len(data) > 0 {
			if item, ok := data[0].(map[string]any); ok {
				for key, value := range typed {
					if _, exists := item[key]; !exists {
						item[key] = value
					}
				}
				return item
			}
		}
		return typed
	default:
		return map[string]any{}
	}
}

func readNormalizedVideoError(payload []byte) string {
	parsed := parseVideoTaskPayload(payload, "")
	if parsed.Status == "failed" || parsed.Error != "" {
		return firstNonEmpty(parsed.Error, "视频任务生成失败")
	}
	return ""
}

func readProviderPayloadError(payload []byte) string {
	var value map[string]any
	if len(payload) == 0 || json.Unmarshal(payload, &value) != nil {
		return ""
	}
	code, hasCode := value["code"]
	if !hasCode {
		return ""
	}
	successCode := false
	switch typed := code.(type) {
	case float64:
		successCode = typed == 0 || typed == 200
	case string:
		text := strings.TrimSpace(strings.ToLower(typed))
		successCode = text == "" || text == "0" || text == "200" || text == "success" || text == "ok"
	default:
		successCode = false
	}
	if successCode {
		return ""
	}
	return firstNonEmpty(readStringPath(value, "error.message"), readStringPath(value, "error"), readStringPath(value, "message"), readStringPath(value, "msg"), fmt.Sprint(code))
}

func readStringPath(data map[string]any, path string) string {
	var current any = data
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[part]
	}
	return strings.TrimSpace(toStringSafe(current))
}

func readIntPath(data map[string]any, key string) int {
	value := data[key]
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	case string:
		var number int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &number)
		return number
	default:
		return 0
	}
}

func readSizeFromDimensions(data map[string]any) string {
	width := readIntPath(data, "width")
	height := readIntPath(data, "height")
	if width > 0 && height > 0 {
		return fmt.Sprintf("%dx%d", width, height)
	}
	return ""
}

func findFirstHTTPURL(value any) string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
			return text
		}
		var parsed any
		if json.Unmarshal([]byte(text), &parsed) == nil {
			return findFirstHTTPURL(parsed)
		}
	case []any:
		for _, item := range typed {
			if url := findFirstHTTPURL(item); url != "" {
				return url
			}
		}
	case map[string]any:
		for _, key := range []string{"url", "video_url", "videoUrl", "download_url", "downloadUrl", "output_url", "outputUrl", "resultUrls", "result_urls", "videoUrls", "video_urls", "urls", "videos", "video", "content", "data", "result", "metadata"} {
			if url := findFirstHTTPURL(typed[key]); url != "" {
				return url
			}
		}
	}
	return ""
}

func refundVideoCredits(userID string, modelName string, credits int, endpoint string) {
	if err := service.RefundUserCredits(userID, modelName, credits, endpoint); err != nil {
		log.Printf("AI video refund credits failed: user=%s model=%s credits=%d err=%v", userID, modelName, credits, err)
	}
}

func readVideoBillingParameters(body []byte, contentType string) (string, int) {
	values := map[string]any{}
	if strings.HasPrefix(contentType, "multipart/form-data") {
		_, params, err := mime.ParseMediaType(contentType)
		if err == nil {
			form, readErr := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(32 << 20)
			if readErr == nil {
				defer form.RemoveAll()
				for key, items := range form.Value {
					if len(items) > 0 {
						values[key] = items[0]
					}
				}
			}
		}
	} else {
		_ = json.Unmarshal(body, &values)
	}
	resolution := normalizeVideoBillingResolution(firstBillingString(values, "resolution_name", "resolution", "vquality", "quality"), firstBillingString(values, "size"), billingInt(values["width"]), billingInt(values["height"]))
	seconds := billingInt(firstBillingValue(values, "seconds", "duration", "video_seconds"))
	if seconds <= 0 {
		frames := billingInt(values["num_frames"])
		frameRate := billingInt(values["frame_rate"])
		if frames > 1 && frameRate > 0 {
			seconds = int(math.Ceil(float64(frames-1) / float64(frameRate)))
		}
	}
	return resolution, seconds
}

func normalizeVideoBillingResolution(value string, size string, width int, height int) string {
	selected := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	switch selected {
	case "low":
		return "480p"
	case "auto", "high", "medium":
		return "720p"
	}
	if selected != "" {
		if _, err := strconv.Atoi(selected); err == nil {
			return selected + "p"
		}
		return selected
	}
	if width <= 0 || height <= 0 {
		_, _ = fmt.Sscanf(strings.ToLower(strings.TrimSpace(size)), "%dx%d", &width, &height)
	}
	shortSide := width
	if shortSide <= 0 || (height > 0 && height < shortSide) {
		shortSide = height
	}
	if shortSide >= 900 {
		return "1080p"
	}
	if shortSide >= 600 {
		return "720p"
	}
	if shortSide > 0 {
		return "480p"
	}
	return ""
}

func firstBillingString(values map[string]any, keys ...string) string {
	value := firstBillingValue(values, keys...)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstBillingValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return value
		}
	}
	return nil
}

func billingInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(math.Ceil(typed))
	case int:
		return typed
	case string:
		number, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(typed, "s")), 64)
		return int(math.Ceil(number))
	default:
		return 0
	}
}
