// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	host = flag.String("host", "localhost", "host to listen on")
	port = flag.String("port", "8080", "port to listen on")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "This program runs MCP servers over streamable HTTP.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEndpoints:\n")
		fmt.Fprintf(os.Stderr, "  /greeter1 - Greeter 1 service\n")
		fmt.Fprintf(os.Stderr, "  /greeter2 - Greeter 2 service\n")
		os.Exit(1)
	}
	flag.Parse()

	addr := fmt.Sprintf("%s:%s", *host, *port)

	server1 := server.NewMCPServer("greeter1", "0.0.1", server.WithToolCapabilities(true))
	server1.AddTool(mcp.NewTool("greet1", mcp.WithDescription("say hi"), mcp.WithString("name", mcp.Required())),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, _ := req.RequireString("name")
			return mcp.NewToolResultText("Hi " + name), nil
		})

	server2 := server.NewMCPServer("greeter2", "0.0.1", server.WithToolCapabilities(true))
	server2.AddTool(mcp.NewTool("greet2", mcp.WithDescription("say hello"), mcp.WithString("name", mcp.Required())),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, _ := req.RequireString("name")
			return mcp.NewToolResultText("Hello " + name), nil
		})

	mux := http.NewServeMux()
	mux.Handle("/greeter1", server.NewStreamableHTTPServer(server1))
	mux.Handle("/greeter2", server.NewStreamableHTTPServer(server2))

	log.Printf("MCP servers serving at http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
