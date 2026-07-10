package protocol

import (
	"encoding/json"
	"testing"
)

func TestOKResponse(t *testing.T) {
	resp := OKResponse(1, map[string]string{"answer": "42"})
	if resp.JSONRPC != "2.0" || resp.ID != 1 || resp.Error != nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if result["answer"] != "42" {
		t.Errorf("result = %v", result)
	}
	// The whole response must marshal to valid JSON-RPC.
	if _, err := json.Marshal(resp); err != nil {
		t.Fatalf("marshal response: %v", err)
	}
}

func TestErrorResponse(t *testing.T) {
	resp := ErrorResponse("req-7", -32601, "method not found", map[string]any{"method": "foo"})
	if resp.JSONRPC != "2.0" || resp.ID != "req-7" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Error == nil || resp.Error.Code != -32601 || resp.Error.Message != "method not found" {
		t.Fatalf("error object = %+v", resp.Error)
	}
	if len(resp.Result) != 0 {
		t.Errorf("error response should have no result, got %s", resp.Result)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// omitempty: a success "result" field must be absent on an error response.
	if got := string(b); contains(got, `"result"`) {
		t.Errorf("error response leaked a result field: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
