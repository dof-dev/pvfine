// Package mcpserver contains the local MCP transport; archive logic lives in services.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Tool struct {
	Name, Description string
	Schema            map[string]any
	ReadOnly          bool
	Call              func(context.Context, json.RawMessage) (any, error)
}

func Handler(definitions []Tool) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "pvfine", Version: "1.0.0"}, nil)
	for _, definition := range definitions {
		d := definition
		data, _ := json.Marshal(d.Schema)
		var schema jsonschema.Schema
		if err := json.Unmarshal(data, &schema); err != nil {
			panic(err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			panic(err)
		}
		server.AddTool(&mcp.Tool{Name: d.Name, Description: d.Description, InputSchema: d.Schema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: d.ReadOnly}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			raw := request.Params.Arguments
			if len(raw) == 0 {
				raw = json.RawMessage(`{}`)
			}
			var input any
			if json.Unmarshal(raw, &input) != nil || resolved.Validate(input) != nil {
				return result(nil, fmt.Errorf("INVALID_ARGUMENT: 参数不符合工具 schema")), nil
			}
			value, callErr := d.Call(ctx, raw)
			return result(value, callErr), nil
		})
	}
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "Forbidden", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != r.Host {
				http.Error(w, "Forbidden", 403)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		transport.ServeHTTP(w, r)
	})
}

func result(value any, err error) *mcp.CallToolResult {
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
	}
	data, e := json.Marshal(value)
	if e != nil {
		return result(nil, fmt.Errorf("INTERNAL: 结果编码失败"))
	}
	if len(data) > 2<<20 {
		return result(nil, fmt.Errorf("RESULT_TOO_LARGE: 请缩小范围或使用分页"))
	}
	return &mcp.CallToolResult{StructuredContent: map[string]any{"result": value}, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}

func Serve(listener net.Listener, handler http.Handler) *http.Server {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	go func() { _ = server.Serve(listener) }()
	return server
}
