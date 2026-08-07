package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
)

type miniMaxVideoContent struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *miniMaxMediaURL `json:"image_url,omitempty"`
	VideoURL *miniMaxMediaURL `json:"video_url,omitempty"`
	AudioURL *miniMaxMediaURL `json:"audio_url,omitempty"`
	Role     string           `json:"role,omitempty"`
}

type miniMaxMediaURL struct {
	URL string `json:"url"`
}

func isMiniMaxVideoChannel(channel model.ModelChannel, modelName string) bool {
	return strings.EqualFold(strings.TrimSpace(channel.Protocol), "minimax") ||
		(strings.EqualFold(strings.TrimSpace(modelName), "MiniMax-H3") && strings.Contains(strings.ToLower(channel.BaseURL), "api.minimaxi.com"))
}

func normalizeMiniMaxVideoBody(body []byte, contentType string, modelName string) ([]byte, string, error) {
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return nil, "", errors.New("MiniMax 视频请求格式无效")
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", err
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(64 << 20)
	if err != nil {
		return nil, "", err
	}
	defer form.RemoveAll()
	if len(form.File) > 0 {
		return nil, "", errors.New("MiniMax 官方渠道仅支持公网素材地址，请先将本地素材上传到云存储")
	}
	prompt := miniMaxFormValue(form, "prompt")
	if prompt == "" {
		return nil, "", errors.New("MiniMax 视频提示词不能为空")
	}
	content := []miniMaxVideoContent{{Type: "text", Text: prompt}}
	firstFrame := miniMaxFormValue(form, "first_frame_url")
	lastFrame := miniMaxFormValue(form, "last_frame_url")
	if firstFrame != "" || lastFrame != "" {
		if firstFrame != "" {
			if err := validateMiniMaxPublicURL(firstFrame); err != nil {
				return nil, "", err
			}
			content = append(content, miniMaxVideoContent{Type: "image_url", ImageURL: &miniMaxMediaURL{URL: firstFrame}, Role: "first_frame"})
		}
		if lastFrame != "" {
			if err := validateMiniMaxPublicURL(lastFrame); err != nil {
				return nil, "", err
			}
			content = append(content, miniMaxVideoContent{Type: "image_url", ImageURL: &miniMaxMediaURL{URL: lastFrame}, Role: "last_frame"})
		}
	} else {
		for _, value := range form.Value["input_reference[]"] {
			if err := validateMiniMaxPublicURL(value); err != nil {
				return nil, "", err
			}
			content = append(content, miniMaxVideoContent{Type: "image_url", ImageURL: &miniMaxMediaURL{URL: value}, Role: "reference_image"})
		}
		for _, value := range form.Value["video_reference[]"] {
			if err := validateMiniMaxPublicURL(value); err != nil {
				return nil, "", err
			}
			content = append(content, miniMaxVideoContent{Type: "video_url", VideoURL: &miniMaxMediaURL{URL: value}, Role: "reference_video"})
		}
		for _, value := range form.Value["audio_reference[]"] {
			if err := validateMiniMaxPublicURL(value); err != nil {
				return nil, "", err
			}
			content = append(content, miniMaxVideoContent{Type: "audio_url", AudioURL: &miniMaxMediaURL{URL: value}, Role: "reference_audio"})
		}
	}
	duration, _ := strconv.Atoi(miniMaxFormValue(form, "seconds"))
	if duration < 4 {
		duration = 4
	}
	if duration > 15 {
		duration = 15
	}
	resolution := "768P"
	if strings.EqualFold(miniMaxFormValue(form, "resolution_name"), "2K") {
		resolution = "2K"
	}
	ratio := normalizeMiniMaxRatio(miniMaxFormValue(form, "size"))
	if firstFrame != "" || lastFrame != "" {
		ratio = "adaptive"
	}
	payload := map[string]any{
		"model": "MiniMax-H3", "content": content, "resolution": resolution,
		"duration": duration, "ratio": ratio,
	}
	if strings.EqualFold(miniMaxFormValue(form, "aigc_watermark"), "true") {
		payload["aigc_watermark"] = true
	}
	encoded, err := json.Marshal(payload)
	return encoded, "application/json", err
}

func transformMiniMaxCreateVideoResponse(payload []byte, modelName string) ([]byte, bool) {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if json.Unmarshal(payload, &input) != nil || input.TaskID == "" {
		return nil, false
	}
	encoded, err := json.Marshal(map[string]any{"id": input.TaskID, "task_id": input.TaskID, "model": modelName, "status": "queued"})
	return encoded, err == nil
}

func transformMiniMaxVideoTaskResponse(payload []byte) ([]byte, bool) {
	var input struct {
		Task struct {
			ID         string `json:"id"`
			Status     string `json:"status"`
			Resolution string `json:"resolution"`
			Duration   int    `json:"duration"`
			Ratio      string `json:"ratio"`
			Content    struct {
				URL string `json:"url"`
			} `json:"content"`
		} `json:"task"`
	}
	if json.Unmarshal(payload, &input) != nil || input.Task.ID == "" {
		return nil, false
	}
	result := map[string]any{"id": input.Task.ID, "task_id": input.Task.ID, "status": input.Task.Status, "resolution": input.Task.Resolution, "duration": input.Task.Duration, "size": input.Task.Ratio}
	if input.Task.Content.URL != "" {
		result["video_url"] = input.Task.Content.URL
	}
	encoded, err := json.Marshal(result)
	return encoded, err == nil
}

func miniMaxFormValue(form *multipart.Form, key string) string {
	values := form.Value[key]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func validateMiniMaxPublicURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("MiniMax 官方渠道仅支持可公网访问的 HTTP(S) 素材地址")
	}
	return nil
}

func normalizeMiniMaxRatio(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		return normalized
	case "1792x1024", "1280x720":
		return "16:9"
	case "1024x1792", "720x1280":
		return "9:16"
	case "1024x1024":
		return "1:1"
	default:
		return "16:9"
	}
}
