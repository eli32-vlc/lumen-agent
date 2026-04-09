package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"element-orion/internal/config"
)

var mcpToolNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

type mcpBoundTool struct {
	serverName string
	toolName   string
	session    *mcp.ClientSession
	timeout    time.Duration
	locks      *resourceLockManager
}

func (r *Registry) registerMCPTools(ctx context.Context) error {
	for _, server := range r.cfg.MCP.Servers {
		if !server.Enabled {
			continue
		}

		if err := r.registerMCPServerTools(ctx, server); err != nil {
			return err
		}
	}

	return nil
}

func (r *Registry) registerMCPServerTools(ctx context.Context, server config.MCPServerConfig) error {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "element-orion",
		Version: "1.0.0",
	}, nil)

	transport, err := r.mcpTransport(server)
	if err != nil {
		return fmt.Errorf("configure MCP server %q: %w", server.Name, err)
	}

	startupCtx, cancel := context.WithTimeout(ctx, r.cfg.MCPServerStartupTimeout(server))
	defer cancel()

	session, err := client.Connect(startupCtx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect MCP server %q: %w", server.Name, err)
	}

	r.close = append(r.close, session.Close)

	toolsResult, err := session.ListTools(startupCtx, nil)
	if err != nil {
		return fmt.Errorf("list tools from MCP server %q: %w", server.Name, err)
	}

	for _, tool := range toolsResult.Tools {
		if tool == nil {
			continue
		}

		toolAlias := mcpToolAlias(server.Name, tool.Name)
		parameters, err := mcpToolParameters(tool.InputSchema)
		if err != nil {
			return fmt.Errorf("prepare schema for MCP tool %q from server %q: %w", tool.Name, server.Name, err)
		}

		bound := &mcpBoundTool{
			serverName: server.Name,
			toolName:   tool.Name,
			session:    session,
			timeout:    r.cfg.MCPServerToolTimeout(server),
			locks:      r.ensureLockManager(),
		}

		description := strings.TrimSpace(tool.Description)
		if description == "" {
			description = fmt.Sprintf("MCP tool %s from server %s", tool.Name, server.Name)
		} else {
			description = fmt.Sprintf("[MCP:%s] %s", server.Name, description)
		}

		if err := r.registerAlways(toolAlias, description, parameters, bound.handle); err != nil {
			return fmt.Errorf("register MCP tool %q from server %q: %w", tool.Name, server.Name, err)
		}
	}

	return nil
}

func (r *Registry) mcpTransport(server config.MCPServerConfig) (mcp.Transport, error) {
	switch server.Transport {
	case "stdio":
		cmd := exec.Command(server.Command, server.Args...)
		cmd.Dir = r.root
		if strings.TrimSpace(server.WorkingDir) != "" {
			cmd.Dir = server.WorkingDir
		}
		if len(server.Env) > 0 {
			env := os.Environ()
			for key, value := range server.Env {
				env = append(env, key+"="+value)
			}
			cmd.Env = env
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	case "http", "streamable_http":
		return &mcp.StreamableClientTransport{
			Endpoint:   server.Endpoint,
			HTTPClient: &http.Client{Timeout: r.cfg.MCPServerToolTimeout(server)},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", server.Transport)
	}
}

func mcpToolAlias(serverName string, toolName string) string {
	return "mcp__" + sanitizeMCPToolName(serverName) + "__" + sanitizeMCPToolName(toolName)
}

func sanitizeMCPToolName(value string) string {
	cleaned := strings.Trim(mcpToolNameSanitizer.ReplaceAllString(value, "_"), "_")
	if cleaned == "" {
		return "tool"
	}
	return cleaned
}

func mcpToolParameters(inputSchema any) (map[string]any, error) {
	if inputSchema == nil {
		return objectSchema(map[string]any{}), nil
	}
	if schema, ok := inputSchema.(map[string]any); ok {
		return schema, nil
	}

	data, err := json.Marshal(inputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema: %w", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("decode input schema: %w", err)
	}
	return schema, nil
}

func (t *mcpBoundTool) handle(ctx context.Context, payload json.RawMessage) (string, error) {
	var arguments map[string]any
	if len(strings.TrimSpace(string(payload))) == 0 {
		arguments = map[string]any{}
	} else if err := json.Unmarshal(payload, &arguments); err != nil {
		return "", fmt.Errorf("decode MCP tool arguments: %w", err)
	}

	release, err := t.locks.Acquire(ctx, "mcp:"+strings.TrimSpace(t.serverName))
	if err != nil {
		return "", err
	}
	defer release()

	toolCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	result, err := t.session.CallTool(toolCtx, &mcp.CallToolParams{
		Name:      t.toolName,
		Arguments: arguments,
	})
	if err != nil {
		return "", fmt.Errorf("call MCP tool %s on server %s: %w", t.toolName, t.serverName, err)
	}

	data, err := json.MarshalIndent(map[string]any{
		"server": t.serverName,
		"tool":   t.toolName,
		"result": result,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode MCP tool result: %w", err)
	}

	return string(data), nil
}
