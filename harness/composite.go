package harness

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ruromero/la-fabriquilla/inference"
	"github.com/ruromero/la-fabriquilla/sandbox"
)

type CompositeToolHandler struct {
	routes map[string]inference.ToolHandler
}

func NewCompositeToolHandler() *CompositeToolHandler {
	return &CompositeToolHandler{routes: make(map[string]inference.ToolHandler)}
}

func (c *CompositeToolHandler) Register(tools []inference.Tool, handler inference.ToolHandler) {
	for _, t := range tools {
		c.routes[t.Function.Name] = handler
	}
}

func (c *CompositeToolHandler) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	h, ok := c.routes[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	result, err := h.Execute(ctx, name, args)
	if err != nil {
		redactedMsg, errEvents := sandbox.RedactSecrets(err.Error())
		logRedactionEvents("tool error", name, errEvents)
		return "", fmt.Errorf("%s", redactedMsg)
	}
	redacted, events := sandbox.RedactSecrets(result)
	logRedactionEvents("tool response", name, events)
	return redacted, nil
}

func logRedactionEvents(source, tool string, events []sandbox.RedactionEvent) {
	if len(events) == 0 {
		return
	}
	patterns := make([]string, len(events))
	for i, e := range events {
		patterns[i] = fmt.Sprintf("%s(%d,line:%d)", e.Pattern, e.Count, e.FirstLine)
	}
	slog.Warn("credentials redacted from "+source,
		"tool", tool,
		"patterns", strings.Join(patterns, ", "),
	)
}

func FilterTools(tools []inference.Tool, allowed map[string]bool) []inference.Tool {
	var filtered []inference.Tool
	for _, t := range tools {
		if allowed[t.Function.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
