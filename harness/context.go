package harness

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ruromero/la-fabriquilla/github"
)

type RepoContext struct {
	docs                  map[string]string
	sections              map[string][]Section
	agentInstructionsFile string
}

var contextDocs = []string{
	"README.md",
	"ARCHITECTURE.md",
	"CONVENTIONS.md",
}

func LoadRepoContext(ctx context.Context, gh github.Service, agentInstructionsFile string) (*RepoContext, error) {
	rc := &RepoContext{
		docs:                  make(map[string]string),
		sections:              make(map[string][]Section),
		agentInstructionsFile: agentInstructionsFile,
	}

	for _, name := range contextDocs {
		content, err := gh.GetFileContent(ctx, name)
		if err != nil {
			slog.Warn("could not load repo context file", "file", name, "error", err)
			continue
		}
		rc.docs[name] = content
		rc.sections[name] = ParseSections(content)
	}

	if agentInstructionsFile != "" {
		content, err := gh.GetFileContent(ctx, agentInstructionsFile)
		if err != nil {
			return nil, fmt.Errorf("load agent instructions %s: %w", agentInstructionsFile, err)
		}
		rc.docs[agentInstructionsFile] = content
		rc.sections[agentInstructionsFile] = ParseSections(content)
	}

	return rc, nil
}

func (rc *RepoContext) allDocNames() []string {
	names := make([]string, len(contextDocs))
	copy(names, contextDocs)
	if rc.agentInstructionsFile != "" {
		names = append(names, rc.agentInstructionsFile)
	}
	return names
}

func (rc *RepoContext) Summaries() string {
	var b strings.Builder
	for _, name := range rc.allDocNames() {
		content, ok := rc.docs[name]
		if !ok {
			continue
		}
		summary := ExtractSummary(content)
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", name, summary)
	}
	return strings.TrimSpace(b.String())
}

func (rc *RepoContext) Conventions() string {
	return rc.docs["CONVENTIONS.md"]
}

func (rc *RepoContext) ListDocuments() []string {
	var names []string
	for _, name := range rc.allDocNames() {
		if _, ok := rc.docs[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

func (rc *RepoContext) ListSections(doc string) ([]string, error) {
	sections, ok := rc.sections[doc]
	if !ok {
		return nil, fmt.Errorf("document %q not found", doc)
	}
	return SectionNames(sections), nil
}

func (rc *RepoContext) GetSection(doc, section string) (string, error) {
	sections, ok := rc.sections[doc]
	if !ok {
		return "", fmt.Errorf("document %q not found", doc)
	}
	content, ok := FindSection(sections, section)
	if !ok {
		return "", fmt.Errorf("section %q not found in %s", section, doc)
	}
	return content, nil
}

func (rc *RepoContext) GetFullDocument(doc string) (string, error) {
	content, ok := rc.docs[doc]
	if !ok {
		return "", fmt.Errorf("document %q not found", doc)
	}
	return content, nil
}
