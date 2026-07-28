package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"
)

func normalizeArkSeedanceVideoBody(body []byte, contentType string, modelName string) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", fmt.Errorf("解析视频请求类型失败: %w", err)
	}
	if mediaType == "application/json" {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, "", fmt.Errorf("解析火山方舟视频请求失败: %w", err)
		}
		if _, ok := payload["content"]; ok {
			return body, "application/json", nil
		}
		model := firstNonEmpty(jsonString(payload["model"]), modelName)
		return marshalArkSeedancePayload(model, jsonString(payload["prompt"]), jsonString(payload["size"]),
			jsonString(payload["resolution_name"]), jsonString(firstValue(payload, "seconds", "duration")),
			jsonBoolPointer(payload["video_generate_audio"]), jsonBoolPointer(payload["watermark"]), nil)
	}
	if mediaType != "multipart/form-data" {
		return nil, "", fmt.Errorf("火山方舟视频接口不支持请求类型 %s", mediaType)
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	form, err := reader.ReadForm(256 << 20)
	if err != nil {
		return nil, "", fmt.Errorf("解析视频表单失败: %w", err)
	}
	defer form.RemoveAll()

	model := formValue(form, "model")
	if model == "" {
		model = modelName
	}
	content := make([]map[string]any, 0, 16)
	if err := appendArkMediaValues(&content, form, "input_reference[]", "image_url", "reference_image", true); err != nil {
		return nil, "", err
	}
	if err := appendArkMediaValues(&content, form, "first_frame_url", "image_url", "first_frame", true); err != nil {
		return nil, "", err
	}
	if err := appendArkMediaValues(&content, form, "last_frame_url", "image_url", "last_frame", true); err != nil {
		return nil, "", err
	}
	if err := appendArkMediaValues(&content, form, "video_reference[]", "video_url", "reference_video", false); err != nil {
		return nil, "", err
	}
	if err := appendArkMediaValues(&content, form, "audio_reference[]", "audio_url", "reference_audio", false); err != nil {
		return nil, "", err
	}

	return marshalArkSeedancePayload(model, formValue(form, "prompt"), formValue(form, "size"),
		formValue(form, "resolution_name"), firstNonEmpty(formValue(form, "seconds"), formValue(form, "duration")),
		formBoolPointer(form, "video_generate_audio"), formBoolPointer(form, "watermark"), content)
}

func marshalArkSeedancePayload(modelName, prompt, ratio, resolution, duration string, generateAudio, watermark *bool, media []map[string]any) ([]byte, string, error) {
	prompt = appendArkPromptOption(prompt, "--ratio", ratio)
	prompt = appendArkPromptOption(prompt, "--resolution", resolution)
	if duration != "" && duration != "-1" {
		prompt = appendArkPromptOption(prompt, "--dur", duration)
	}
	content := make([]map[string]any, 0, len(media)+1)
	content = append(content, map[string]any{"type": "text", "text": prompt})
	content = append(content, media...)
	payload := map[string]any{"model": modelName, "content": content}
	if generateAudio != nil {
		payload["generate_audio"] = *generateAudio
	}
	if watermark != nil {
		payload["watermark"] = *watermark
	}
	result, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("生成火山方舟视频请求失败: %w", err)
	}
	return result, "application/json", nil
}

func appendArkMediaValues(content *[]map[string]any, form *multipart.Form, field, mediaType, role string, allowFile bool) error {
	property := mediaType
	for _, value := range form.Value[field] {
		value = strings.TrimSpace(value)
		if value != "" {
			*content = append(*content, map[string]any{
				"type": mediaType, property: map[string]any{"url": value}, "role": role,
			})
		}
	}
	for _, header := range form.File[field] {
		if !allowFile {
			return fmt.Errorf("火山方舟参考视频和音频必须使用可公开访问的 URL")
		}
		value, err := arkMultipartFileDataURL(header)
		if err != nil {
			return err
		}
		*content = append(*content, map[string]any{
			"type": mediaType, property: map[string]any{"url": value}, "role": role,
		})
	}
	return nil
}

func arkMultipartFileDataURL(header *multipart.FileHeader) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", fmt.Errorf("读取参考图片失败: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32<<20))
	if err != nil {
		return "", fmt.Errorf("读取参考图片失败: %w", err)
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func appendArkPromptOption(prompt, option, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(prompt, option) {
		return strings.TrimSpace(prompt)
	}
	return strings.TrimSpace(prompt + " " + option + " " + value)
}

func formValue(form *multipart.Form, key string) string {
	if values := form.Value[key]; len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

func formBoolPointer(form *multipart.Form, key string) *bool {
	value := formValue(form, key)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func jsonBoolPointer(value any) *bool {
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func firstValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}
