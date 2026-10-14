package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ base http.RoundTripper }

func (b bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+os.Getenv("DINERO_API_TOKEN"))
	return b.base.RoundTrip(request)
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "dinero-container-smoke", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: os.Getenv("DINERO_MCP_URL"), DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: bearer{base: http.DefaultTransport}},
	}, nil)
	if err != nil {
		panic(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		panic(err)
	}
	if len(listed.Tools) != 6 {
		panic(fmt.Sprintf("tool count %d", len(listed.Tools)))
	}
	called, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_balance", Arguments: map[string]any{"account": "Test Account", "through": "2026-09-02"},
	})
	if err != nil {
		panic(err)
	}
	if called.IsError {
		panic(fmt.Sprintf("tool error: %#v", called))
	}
	fmt.Printf("MCP initialize/list/call passed: %d tools; balance=%#v\n", len(listed.Tools), called.StructuredContent)
}
