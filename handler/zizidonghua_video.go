package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func isZizidonghuaVideoChannel(channel model.ModelChannel) bool {
	return strings.EqualFold(strings.TrimSpace(channel.Protocol), "zizidonghua") || strings.Contains(strings.ToLower(channel.BaseURL), "zizidonghua.com")
}

func transformZizidonghuaVideoResponse(payload []byte) ([]byte, bool) {
	var root map[string]any
	if json.Unmarshal(payload, &root) != nil {
		return payload, false
	}
	outer, _ := root["data"].(map[string]any)
	if outer == nil {
		return payload, false
	}
	inner, _ := outer["data"].(map[string]any)
	result := map[string]any{}
	for key, value := range outer {
		if key != "data" {
			result[key] = value
		}
	}
	for key, value := range inner {
		result[key] = value
	}
	result["task_id"] = firstNonEmpty(toStringSafe(result["task_id"]), toStringSafe(outer["task_id"]), toStringSafe(root["task_id"]))
	result["status"] = normalizeZizidonghuaStatus(firstNonEmpty(toStringSafe(inner["status"]), toStringSafe(outer["status"]), toStringSafe(root["status"])))
	result["progress"] = zizidonghuaProgress(firstNonEmpty(toStringSafe(outer["progress"]), toStringSafe(inner["progress"])))
	if errorMessage := firstNonEmpty(toStringSafe(inner["error"]), toStringSafe(outer["fail_reason"]), toStringSafe(root["message"])); errorMessage != "" {
		result["error"] = errorMessage
	}
	if videoURL := zizidonghuaVideoURL(inner, outer); videoURL != "" {
		result["video_url"] = videoURL
		result["status"] = "completed"
		result["progress"] = 100
	}
	transformed, err := json.Marshal(result)
	return transformed, err == nil
}

func normalizeZizidonghuaStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "NOT_START", "NOT_STARTED", "SUBMITTED", "PENDING", "QUEUED", "QUEUE", "IN_QUEUE":
		return "queued"
	case "IN_PROGRESS", "PROCESSING", "RUNNING", "DISPATCHED", "ACCEPTED", "STARTED", "GENERATING":
		return "processing"
	case "SUCCESS", "SUCCEEDED", "COMPLETED", "COMPLETE", "DONE":
		return "completed"
	case "FAILED", "FAIL", "ERROR", "CANCELLED", "CANCELED":
		return "failed"
	default:
		normalized := service.NormalizeVideoTaskStatus(value)
		if normalized == "queued" || normalized == "processing" || normalized == "completed" || normalized == "failed" {
			return normalized
		}
		return "processing"
	}
}

func zizidonghuaProgress(value string) int {
	var progress int
	_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &progress)
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func zizidonghuaVideoURL(values ...map[string]any) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		for _, key := range []string{"video_url", "output_url", "download_url", "url"} {
			if videoURL := strings.TrimSpace(toStringSafe(value[key])); strings.HasPrefix(videoURL, "http://") || strings.HasPrefix(videoURL, "https://") {
				return videoURL
			}
		}
		for _, key := range []string{"result", "output", "video", "content"} {
			if videoURL := findFirstHTTPURL(value[key]); videoURL != "" {
				return videoURL
			}
		}
	}
	return ""
}

func normalizeZizidonghuaVideoBody(body []byte, contentType string, modelName string) ([]byte, string, error) {
	payload := map[string]any{}
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if mediaType == "multipart/form-data" {
		form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(32 << 20)
		if err != nil {
			return nil, "", errors.New("字字动画视频请求格式无效")
		}
		defer form.RemoveAll()
		if len(form.File) > 0 {
			return nil, "", errors.New("字字动画仅支持公网素材地址，请先将本地素材上传到云存储")
		}
		for key, values := range form.Value {
			if len(values) == 1 {
				payload[key] = values[0]
			} else if len(values) > 1 {
				payload[key] = values
			}
		}
	} else if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", errors.New("字字动画视频请求格式无效")
	}

	prompt := strings.TrimSpace(toStringSafe(payload["prompt"]))
	if prompt == "" {
		return nil, "", errors.New("字字动画视频提示词不能为空")
	}
	result := map[string]any{"model": modelName, "prompt": prompt}
	if duration := normalizeAPIMartInt(firstNonEmptyValue(payload["duration"], payload["seconds"])); duration > 0 {
		result["duration"] = duration
	}
	ratio := zizidonghuaAspectRatio(firstNonEmpty(toStringSafe(payload["aspect_ratio"]), toStringSafe(payload["size"])))
	if ratio != "" {
		result["aspect_ratio"] = ratio
	}
	if negativePrompt := strings.TrimSpace(toStringSafe(payload["negative_prompt"])); negativePrompt != "" {
		result["negative_prompt"] = negativePrompt
	}

	references := make([]map[string]any, 0)
	appendReferences := func(value any, role string) error {
		if items, ok := value.([]any); ok {
			for _, rawItem := range items {
				item, ok := rawItem.(map[string]any)
				if !ok {
					continue
				}
				mediaURL := firstNonEmpty(toStringSafe(item["url"]), toStringSafe(item["base64"]))
				if strings.HasPrefix(mediaURL, "data:") {
					reference := map[string]any{"base64": mediaURL}
					if resolvedRole := firstNonEmpty(toStringSafe(item["role"]), role); resolvedRole != "" {
						reference["role"] = resolvedRole
					}
					references = append(references, reference)
					continue
				}
				parsed, err := url.Parse(strings.TrimSpace(mediaURL))
				if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
					return errors.New("字字动画仅支持可公网访问的 HTTP(S) 素材地址")
				}
				reference := map[string]any{"url": parsed.String()}
				if resolvedRole := firstNonEmpty(toStringSafe(item["role"]), role); resolvedRole != "" {
					reference["role"] = resolvedRole
				}
				references = append(references, reference)
			}
			return nil
		}
		for _, raw := range normalizeAPIMartReferenceStringList(value) {
			mediaURL, err := url.Parse(strings.TrimSpace(raw))
			if err != nil || (mediaURL.Scheme != "http" && mediaURL.Scheme != "https") || mediaURL.Host == "" {
				return errors.New("字字动画仅支持可公网访问的 HTTP(S) 素材地址")
			}
			item := map[string]any{"url": mediaURL.String()}
			if role != "" {
				item["role"] = role
			}
			references = append(references, item)
		}
		return nil
	}
	for _, key := range []string{"input_reference[]", "input_reference", "reference_images", "image_urls"} {
		if err := appendReferences(payload[key], ""); err != nil {
			return nil, "", err
		}
	}
	if err := appendReferences(payload["first_frame_url"], "first_frame"); err != nil {
		return nil, "", err
	}
	if err := appendReferences(payload["last_frame_url"], "last_frame"); err != nil {
		return nil, "", err
	}
	if len(references) > 0 {
		result["reference_images"] = references
	}
	encoded, err := json.Marshal(result)
	return encoded, "application/json", err
}

func firstNonEmptyValue(values ...any) any {
	for _, value := range values {
		if strings.TrimSpace(toStringSafe(value)) != "" {
			return value
		}
	}
	return nil
}

func zizidonghuaAspectRatio(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "16:9", "9:16", "1:1":
		return value
	}
	var width, height int
	if _, err := fmt.Sscanf(value, "%dx%d", &width, &height); err != nil || width <= 0 || height <= 0 {
		return ""
	}
	if width == height {
		return "1:1"
	}
	if width > height {
		return "16:9"
	}
	return "9:16"
}
