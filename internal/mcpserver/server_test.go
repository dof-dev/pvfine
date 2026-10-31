package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTransportOriginAndUnauthenticatedDiscovery(t *testing.T) {
	h := Handler([]Tool{{Name: "pvf_test", Description: "test", ReadOnly: true, Schema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false}, Call: func(_ context.Context, raw json.RawMessage) (any, error) { return map[string]bool{"ok": true}, nil }}})
	for _, test := range []struct {
		host, origin string
		status       int
	}{{"evil.example", "", 403}, {"127.0.0.1:1234", "https://evil.example", 403}} {
		r := httptest.NewRequest("POST", "http://"+test.host+"/mcp", nil)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("unexpected HTTP status %d", w.Code)
		}
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 {
		t.Fatal("tool discovery failed")
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pvf_test", Arguments: map[string]any{"value": "hello"}})
	if err != nil || result.IsError {
		t.Fatal("valid tool call failed")
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "pvf_test", Arguments: map[string]any{"value": 5}})
	if err != nil || !result.IsError {
		t.Fatal("invalid schema accepted")
	}
}
