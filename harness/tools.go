package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/inference"
)

type ContextToolHandler struct {
	rc *RepoContext
	gh *github.Client
}

func NewContextToolHandler(rc *RepoContext, gh *github.Client) *ContextToolHandler {
	return &ContextToolHandler{rc: rc, gh: gh}
}

func (h *ContextToolHandler) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	switch name {
	case "list_documents":
		return strings.Join(h.rc.ListDocuments(), "\n"), nil

	case "list_sections":
		doc, _ := args["document"].(string)
		sections, err := h.rc.ListSections(doc)
		if err != nil {
			return "", err
		}
		return strings.Join(sections, "\n"), nil

	case "get_section":
		doc, _ := args["document"].(string)
		section, _ := args["section"].(string)
		return h.rc.GetSection(doc, section)

	case "get_document":
		doc, _ := args["document"].(string)
		return h.rc.GetFullDocument(doc)

	case "read_file":
		path, _ := args["path"].(string)
		return h.gh.GetFileContent(ctx, path)

	case "list_files":
		path, _ := args["path"].(string)
		return h.listFiles(ctx, path)

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *ContextToolHandler) listFiles(ctx context.Context, path string) (string, error) {
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Path string `json:"path"`
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", h.gh.Owner(), h.gh.Repo(), path)
	content, err := h.gh.GetRaw(ctx, url)
	if err != nil {
		return "", err
	}

	var entries []entry
	if err := json.Unmarshal([]byte(content), &entries); err != nil {
		return "", fmt.Errorf("parse directory listing: %w", err)
	}

	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s %s\n", e.Type, e.Path)
	}
	return strings.TrimSpace(b.String()), nil
}

func ContextTools() []inference.Tool {
	return []inference.Tool{
		{
			Type: "function",
			Function: inference.ToolDef{
				Name:        "list_documents",
				Description: "List available project documentation files loaded into context",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
		{
			Type: "function",
			Function: inference.ToolDef{
				Name:        "list_sections",
				Description: "List section names within a documentation file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"document": map[string]any{
							"type":        "string",
							"description": "Document filename (e.g. ARCHITECTURE.md)",
						},
					},
					"required": []string{"document"},
				},
			},
		},
		{
			Type: "function",
			Function: inference.ToolDef{
				Name:        "get_section",
				Description: "Get the content of a specific section from a documentation file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"document": map[string]any{
							"type":        "string",
							"description": "Document filename (e.g. ARCHITECTURE.md)",
						},
						"section": map[string]any{
							"type":        "string",
							"description": "Section name (e.g. Backend structure, Data model)",
						},
					},
					"required": []string{"document", "section"},
				},
			},
		},
		{
			Type: "function",
			Function: inference.ToolDef{
				Name:        "get_document",
				Description: "Get the full content of a documentation file. Prefer get_section for targeted reads.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"document": map[string]any{
							"type":        "string",
							"description": "Document filename (e.g. CONVENTIONS.md)",
						},
					},
					"required": []string{"document"},
				},
			},
		},
		{
			Type: "function",
			Function: inference.ToolDef{
				Name:        "list_files",
				Description: "List files and directories at a path in the repository",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Directory path (e.g. backend/src/handlers, frontend/src/pages). Use empty string for repo root.",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: inference.ToolDef{
				Name:        "read_file",
				Description: "Read the content of a source file from the repository",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "File path relative to repo root (e.g. backend/src/handlers/books.rs)",
						},
					},
					"required": []string{"path"},
				},
			},
		},
	}
}
