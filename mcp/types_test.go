package mcp

import (
	"encoding/json"
	"testing"
)

func TestToolInfo_Roundtrip(t *testing.T) {
	info := ToolInfo{
		Name:        "read_file",
		Description: "Read a file from the filesystem",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Name != info.Name || decoded.Description != info.Description {
		t.Errorf("roundtrip mismatch: got %+v", decoded)
	}
}

func TestToolCallResult_TextContent(t *testing.T) {
	result := ToolCallResult{
		Content: []Content{
			{Type: "text", Text: "hello "},
			{Type: "text", Text: "world"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolCallResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(decoded.Content))
	}
	if decoded.Content[0].Text != "hello " || decoded.Content[1].Text != "world" {
		t.Errorf("content mismatch: %+v", decoded.Content)
	}
}

func TestJSONRPCRequest_Marshal(t *testing.T) {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	expected := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if string(data) != expected {
		t.Errorf("got %s, want %s", data, expected)
	}
}

func TestJSONRPCResponse_WithError(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`

	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("code = %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid Request" {
		t.Errorf("message = %q", resp.Error.Message)
	}
}

func TestInitializeParams_Marshal(t *testing.T) {
	params := initializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo:      clientInfo{Name: "test", Version: "0.1"},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Error("should not be empty")
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", decoded["protocolVersion"])
	}
}
