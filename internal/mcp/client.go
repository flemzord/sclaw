package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// connectTimeout is the maximum duration for connecting to an MCP server
// and completing the initialize handshake.
const connectTimeout = 30 * time.Second

// Client wraps an MCP client connection and caches the server's tool list.
type Client struct {
	name      string
	serverCfg ServerConfig
	logger    *slog.Logger

	mu        sync.Mutex
	inner     *mcpclient.Client
	tools     []mcp.Tool
	connected bool
}

// NewClient creates a new MCP client wrapper for the given server configuration.
// The actual connection is deferred until Connect is called.
func NewClient(name string, cfg ServerConfig, logger *slog.Logger) *Client {
	return &Client{
		name:      name,
		serverCfg: cfg,
		logger:    logger,
	}
}

// Connect establishes the connection, performs the MCP handshake, and caches
// the server's tool list. It is idempotent — subsequent calls are no-ops.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	var (
		inner *mcpclient.Client
		err   error
	)

	if c.serverCfg.IsStdio() {
		inner, err = mcpclient.NewStdioMCPClient(
			c.serverCfg.Command,
			c.serverCfg.Env,
			c.serverCfg.Args...,
		)
	} else {
		var opts []transport.StreamableHTTPCOption
		if len(c.serverCfg.Headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(c.serverCfg.Headers))
		}
		opts = append(opts, transport.WithHTTPTimeout(connectTimeout))
		inner, err = mcpclient.NewStreamableHttpClient(c.serverCfg.URL, opts...)
	}
	if err != nil {
		return fmt.Errorf("mcp: creating client %q: %w", c.name, err)
	}

	// Initialize handshake.
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "sclaw",
		Version: "1.0.0",
	}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}

	if _, err := inner.Initialize(ctx, initReq); err != nil {
		_ = inner.Close()
		return fmt.Errorf("mcp: initializing %q: %w", c.name, err)
	}

	// List available tools.
	toolsResult, err := inner.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = inner.Close()
		return fmt.Errorf("mcp: listing tools for %q: %w", c.name, err)
	}

	c.inner = inner
	c.tools = toolsResult.Tools
	c.connected = true

	c.logger.Info("mcp: connected",
		"server", c.name,
		"tools", len(c.tools),
	)

	return nil
}

// Tools returns the cached list of MCP tools. Returns nil if not connected.
func (c *Client) Tools() []mcp.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools
}

// CallTool invokes a tool on the connected MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	c.mu.Lock()
	inner := c.inner
	c.mu.Unlock()

	if inner == nil {
		return nil, fmt.Errorf("mcp: client %q not connected", c.name)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	return inner.CallTool(ctx, req)
}

// Close shuts down the MCP client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inner == nil {
		return nil
	}

	err := c.inner.Close()
	c.inner = nil
	c.connected = false
	c.tools = nil
	return err
}
