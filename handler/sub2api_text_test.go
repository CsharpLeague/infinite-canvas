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

func TestNormalizeSub2APITextToolMessages(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"创建节点"},{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"create_text_node","arguments":"{\"title\":\"镜头1\"}"}}]},{"role":"tool","tool_call_id":"call-1","name":"create_text_node","content":"{\"ok\":true,\"nodeId\":\"node-1\"}"}],"stream":true}`)
	result, _, err := normalizeSub2APITextBody(body, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	input := payload["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("unexpected input: %s", result)
	}
	call := input[1].(map[string]any)
	output := input[2].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call-1" || call["name"] != "create_text_node" {
		t.Fatalf("unexpected function call: %v", call)
	}
	if output["type"] != "function_call_output" || output["call_id"] != "call-1" || output["output"] != `{"ok":true,"nodeId":"node-1"}` {
		t.Fatalf("unexpected function output: %v", output)
	}
}
