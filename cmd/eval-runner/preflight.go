package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// preflightOllama verifies the configured model exists on an Ollama
// server by querying {root}/api/tags, where root is the base URL with a
// trailing /v1 stripped. Non-Ollama endpoints (404 or unreachable
// /api/tags) are skipped — only a present-but-missing model fails.
func preflightOllama(baseURL, model string) error {
	root := strings.TrimSuffix(strings.TrimSuffix(baseURL, "/"), "/v1")
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Get(root + "/api/tags")
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil // not an Ollama server; skip the check
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil // unexpected shape; skip rather than block
	}
	available := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name == model {
			return nil
		}
		available = append(available, m.Name)
	}
	return fmt.Errorf("model %q not found on %s — available: %s (ollama pull %s)",
		model, root, strings.Join(available, ", "), model)
}
