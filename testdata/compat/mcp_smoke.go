package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "dinero-container-smoke", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: os.Getenv("DINERO_MCP_URL"), DisableStandaloneSSE: true,
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
