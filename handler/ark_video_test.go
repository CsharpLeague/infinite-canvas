package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"testing"
)

func TestNormalizeArkSeedanceVideoMultipart(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"model": "doubao-seedance-1-5-pro-250528", "prompt": "海边日落",
		"size": "16:9", "resolution_name": "720p", "seconds": "5",
		"video_generate_audio": "true", "first_frame_url": "https://example.com/first.png",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	result, contentType, err := normalizeArkSeedanceVideoBody(body.Bytes(), writer.FormDataContentType(), "")
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	var payload struct {
		Model           string           `json:"model"`
		GenerateAudio   bool             `json:"generate_audio"`
		ReturnLastFrame bool             `json:"return_last_frame"`
		Content         []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "doubao-seedance-1-5-pro-250528" || !payload.GenerateAudio || !payload.ReturnLastFrame {
		t.Fatalf("unexpected payload: %s", result)
	}
	if payload.Content[0]["text"] != "海边日落 --ratio 16:9 --resolution 720p --dur 5" {
		t.Fatalf("unexpected prompt: %v", payload.Content[0]["text"])
	}
	if payload.Content[1]["role"] != "first_frame" {
		t.Fatalf("unexpected media: %v", payload.Content[1])
	}
}

func TestNormalizeArkSeedanceOfficialJSONPassThrough(t *testing.T) {
	body := []byte(`{"model":"seedance","content":[{"type":"text","text":"hello"}]}`)
	result, contentType, err := normalizeArkSeedanceVideoBody(body, "application/json", "seedance")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if json.Unmarshal(result, &payload) != nil || payload["return_last_frame"] != true || contentType != "application/json" {
		t.Fatalf("official payload changed: %s", result)
	}
}

func TestParseArkSeedanceVideoURL(t *testing.T) {
	payload := []byte(`{"id":"cgt-123","status":"succeeded","content":{"video_url":"https://example.com/video.mp4","last_frame_url":"https://example.com/last.png"},"duration":4}`)
	result := parseVideoTaskPayload(payload, "doubao-seedance-2-0-fast")
	if result.Status != "completed" || result.VideoURL != "https://example.com/video.mp4" || result.LastFrameURL != "https://example.com/last.png" {
		t.Fatalf("unexpected parsed result: %+v", result)
	}
}
