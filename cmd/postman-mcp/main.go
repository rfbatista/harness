package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type headerFlags map[string]string

func (h headerFlags) String() string {
	if len(h) == 0 {
		return ""
	}
	parts := make([]string, 0, len(h))
	for k, v := range h {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (h headerFlags) Set(value string) error {
	kv := strings.SplitN(value, "=", 2)
	if len(kv) != 2 {
		return fmt.Errorf("invalid header %q: use Key=Value", value)
	}
	key := strings.TrimSpace(kv[0])
	val := strings.TrimSpace(kv[1])
	if key == "" {
		return fmt.Errorf("invalid header %q: empty key", value)
	}
	h[key] = val
	return nil
}

func main() {
	var (
		url        string
		apiKey     string
		toolName   string
		toolArgs   string
		timeoutSec int
		headers    = headerFlags{}
	)

	flag.StringVar(&url, "url", envOr("POSTMAN_MCP_URL", "https://mcp.postman.com/mcp"), "Postman MCP streamable HTTP URL")
	flag.StringVar(&apiKey, "api-key", os.Getenv("POSTMAN_API_KEY"), "Postman API key (optional, can also use POSTMAN_API_KEY)")
	flag.StringVar(&toolName, "tool", "", "Tool name to execute (optional)")
	flag.StringVar(&toolArgs, "args", "{}", "Tool args as JSON object (used with -tool)")
	flag.IntVar(&timeoutSec, "timeout", 60, "HTTP timeout in seconds")
	flag.Var(headers, "header", "Extra HTTP header in Key=Value format (repeatable)")
	flag.Parse()

	if strings.TrimSpace(url) == "" {
		log.Fatal("missing MCP URL: set -url or POSTMAN_MCP_URL")
	}

	if apiKey != "" && headers["X-Api-Key"] == "" && headers["x-api-key"] == "" {
		headers["X-Api-Key"] = apiKey
	}

	httpOptions := []transport.StreamableHTTPCOption{
		transport.WithHTTPTimeout(time.Duration(timeoutSec) * time.Second),
	}
	if len(headers) > 0 {
		httpOptions = append(httpOptions, transport.WithHTTPHeaders(headers))
	}

	c, err := client.NewStreamableHttpClient(url, httpOptions...)
	if err != nil {
		log.Fatalf("create MCP client: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "postman-mcp-cli", Version: "0.1.0"}

	initRes, err := c.Initialize(ctx, initReq)
	if err != nil {
		log.Fatalf("initialize MCP session: %v", err)
	}

	fmt.Printf("Connected to %s\n", url)
	fmt.Printf("Server: %s %s\n", initRes.ServerInfo.Name, initRes.ServerInfo.Version)

	toolsRes, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Printf("Tools (%d):\n", len(toolsRes.Tools))
	for _, t := range toolsRes.Tools {
		fmt.Printf("- %s: %s\n", t.Name, t.Description)
	}

	if strings.TrimSpace(toolName) == "" {
		return
	}

	args := map[string]any{}
	if err := json.Unmarshal([]byte(toolArgs), &args); err != nil {
		log.Fatalf("invalid -args JSON: %v", err)
	}

	callReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}
	callRes, err := c.CallTool(ctx, callReq)
	if err != nil {
		log.Fatalf("call tool %q: %v", toolName, err)
	}

	pretty, err := json.MarshalIndent(callRes, "", "  ")
	if err != nil {
		log.Fatalf("format tool response: %v", err)
	}
	fmt.Println()
	fmt.Printf("Tool result (%s):\n%s\n", toolName, string(pretty))
}

func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
