# MCP read access

`dinero serve` exposes the same read-only ledger data through Streamable HTTP at `/mcp` and REST at `/api`. The MCP endpoint uses the official Go SDK, with a stateless session for each request. Clients should perform MCP initialization, list tools, and call a tool; a plain `GET /mcp` receives HTTP 405 and does not establish a session.

Set `DINERO_API_TOKEN_FILE` to a file containing a separate read token of at least 16 non-whitespace bytes before starting `dinero serve`. By default the listener binds `127.0.0.1:8080`. For a remote listener, set the server host and add the public hostname to `server.allowed_hosts`; configure `server.allowed_origins` for browser clients. Use TLS or a trusted TLS terminating proxy for remote access. Every MCP request, including initialize, must carry `Authorization: Bearer <read-token>` and pass the same Host and Origin checks as REST.

The following Go client connects to a remote endpoint using the official SDK. Supply `DINERO_MCP_URL` (for example, `https://ledger.example.com/mcp`) and `DINERO_API_TOKEN` through your secret manager; do not embed them in source code.

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "os"

    "github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ base http.RoundTripper }

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
    req = req.Clone(req.Context())
    req.Header.Set("Authorization", "Bearer "+os.Getenv("DINERO_API_TOKEN"))
    return b.base.RoundTrip(req)
}

func main() {
    client := mcp.NewClient(&mcp.Implementation{Name: "ledger-reader", Version: "1.0.0"}, nil)
    session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
        Endpoint: os.Getenv("DINERO_MCP_URL"),
        DisableStandaloneSSE: true,
        HTTPClient: &http.Client{Transport: bearer{base: http.DefaultTransport}},
    }, nil)
    if err != nil { panic(err) }
    defer session.Close()
    tools, err := session.ListTools(context.Background(), nil)
    if err != nil { panic(err) }
    fmt.Println("tools:", len(tools.Tools))
    balance, err := session.CallTool(context.Background(), &mcp.CallToolParams{
        Name: "get_balance",
        Arguments: map[string]any{"account": "checking", "through": "2026-03-09"},
    })
    if err != nil { panic(err) }
    fmt.Printf("balance: %#v\n", balance.StructuredContent)
}
```

The six tools are `list_accounts`, `list_categories`, `search_transactions`, `get_balance`, `summarize_transactions`, and `get_status`. All are annotated read-only. Search defaults to 50 rows and accepts up to 500 per page. Summary requires `after` and `before` dates and covers all matching rows. Amounts are decimal strings or `null`; the currency is `null` because the legacy ledger has no currency field. Tool errors contain generic messages so database details are not exposed.

Clients that support only OAuth for remote MCP, and cannot send a static bearer header, cannot authenticate directly to this endpoint. They need a separately operated OAuth-capable gateway that attaches the read token upstream. The API read token is unrelated to Plaid access tokens and Plaid client credentials. No Plaid token, administrative operation, SQL execution, bank linking, or ingestion is exposed as an MCP tool.
