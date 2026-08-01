package handler

import (
	"encoding/json"
	"testing"
)

func TestNormalizeSub2APITextBody(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"你是助手"},{"role":"user","content":[{"type":"text","text":"描述图片"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}],"stream":true}`)
	result, contentType, err := normalizeSub2APITextBody(body, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["instructions"] != "你是助手" || payload["stream"] != true {
		t.Fatalf("unexpected payload: %s", result)
	}
	input := payload["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "input_text" || content[1].(map[string]any)["type"] != "input_image" {
		t.Fatalf("unexpected content: %v", content)
	}
}
