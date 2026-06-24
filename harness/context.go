package harness

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ruromero/la-fabriquilla/github"
)

type RepoContext struct {
	docs        map[string]string
	sections    map[string][]Section
	includeDocs []string
}

func LoadRepoContext(ctx context.Context, gh github.Service, includeDocs []string) *RepoContext {
	rc := &RepoContext{
		docs:        make(map[string]string),
		sections:    make(map[string][]Section),
		includeDocs: includeDocs,
	}

	for _, name := range includeDocs {
		content, err := gh.GetFileContent(ctx, name)
		if err != nil {
			slog.Warn("could not load repo context file", "file", name, "error", err)
			continue
		}
		rc.docs[name] = content
		rc.sections[name] = ParseSections(content)
	}

	if len(rc.docs) == 0 {
		slog.Warn("no context documents loaded", "requested", includeDocs)
	}

	return rc
}

func (rc *RepoContext) Summaries() string {
	var b strings.Builder
	for _, name := range rc.includeDocs {
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
	for _, name := range rc.includeDocs {
		if filepath.Base(name) == "CONVENTIONS.md" {
			return rc.docs[name]
		}
	}
	return ""
}

func (rc *RepoContext) ListDocuments() []string {
	var names []string
	for _, name := range rc.includeDocs {
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
