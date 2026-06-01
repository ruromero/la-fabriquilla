package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/ruromero/la-fabriquilla/inference"
)

// Client communicates with an MCP server over stdio using JSON-RPC 2.0.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	dec    *json.Decoder
	mu     sync.Mutex
	nextID atomic.Int64
	tools  []inference.Tool
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewClient(name string, args ...string) *Client {
	return &Client{
		cmd: exec.Command(name, args...),
	}
}

func (c *Client) SetEnv(env []string) {
	c.cmd.Env = env
}

func (c *Client) Start(ctx context.Context) error {
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	c.dec = json.NewDecoder(c.stdout)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start mcp server: %w", err)
	}

	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "la-fabriquilla",
			"version": "0.1.0",
		},
	}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	return c.refreshTools(ctx)
}

func (c *Client) Stop() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

func (c *Client) Tools() []inference.Tool {
	return c.tools
}

// Execute implements inference.ToolHandler.
func (c *Client) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (c *Client) refreshTools(ctx context.Context) error {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	var toolList struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &toolList); err != nil {
		return fmt.Errorf("decode tools: %w", err)
	}

	c.tools = make([]inference.Tool, len(toolList.Tools))
	for i, t := range toolList.Tools {
		c.tools[i] = inference.Tool{
			Type: "function",
			Function: inference.ToolDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write to mcp: %w", err)
	}

	var resp jsonRPCResponse
	if err := c.dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("read from mcp: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}
