package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPhaseDuration(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		phase string
		want  time.Duration
	}{
		{
			name:  "default when nothing configured",
			cfg:   Config{},
			phase: "coder",
			want:  15 * time.Minute,
		},
		{
			name: "uses global max_phase_duration",
			cfg: Config{
				MaxPhaseDuration: Duration{20 * time.Minute},
			},
			phase: "planner",
			want:  20 * time.Minute,
		},
		{
			name: "per-phase override takes precedence",
			cfg: Config{
				MaxPhaseDuration: Duration{15 * time.Minute},
				PhaseDurations: map[string]Duration{
					"coder": {30 * time.Minute},
				},
			},
			phase: "coder",
			want:  30 * time.Minute,
		},
		{
			name: "falls back to global when phase not in map",
			cfg: Config{
				MaxPhaseDuration: Duration{20 * time.Minute},
				PhaseDurations: map[string]Duration{
					"coder": {30 * time.Minute},
				},
			},
			phase: "planner",
			want:  20 * time.Minute,
		},
		{
			name: "zero per-phase duration falls back to global",
			cfg: Config{
				MaxPhaseDuration: Duration{20 * time.Minute},
				PhaseDurations: map[string]Duration{
					"coder": {0},
				},
			},
			phase: "coder",
			want:  20 * time.Minute,
		},
		{
			name: "zero global falls back to hardcoded default",
			cfg: Config{
				MaxPhaseDuration: Duration{0},
			},
			phase: "gatherer",
			want:  15 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.PhaseDuration(tt.phase)
			if got != tt.want {
				t.Errorf("PhaseDuration(%q) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}

func TestRepoConfigLanguageFields(t *testing.T) {
	data := `{"owner":"acme","repo":"app","language":"go","sandbox_image":"factory-go:latest","token":"t"}`
	var rc RepoConfig
	if err := json.Unmarshal([]byte(data), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc.Language != "go" {
		t.Errorf("Language = %q, want %q", rc.Language, "go")
	}
	if rc.SandboxImage != "factory-go:latest" {
		t.Errorf("SandboxImage = %q, want %q", rc.SandboxImage, "factory-go:latest")
	}
}

func TestSecurityConfigRoundTrip(t *testing.T) {
	data := `{"allow_private_urls":true}`
	var sc SecurityConfig
	if err := json.Unmarshal([]byte(data), &sc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !sc.AllowPrivateURLs {
		t.Error("AllowPrivateURLs should be true")
	}
}

func TestArbiterConfigValidation(t *testing.T) {
	t.Run("base_url without model fails", func(t *testing.T) {
		data := `{"repos":[{"owner":"a","repo":"b","token":"t"}],"arbiter":{"base_url":"https://api.deepseek.com/v1"}}`
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(path)
		if err == nil {
			t.Fatal("expected error when arbiter.base_url set without model")
		}
		if !strings.Contains(err.Error(), "arbiter.model") {
			t.Errorf("error = %q, want mention of arbiter.model", err)
		}
	})

	t.Run("complete arbiter config loads", func(t *testing.T) {
		data := `{"repos":[{"owner":"a","repo":"b","token":"t"}],"arbiter":{"base_url":"https://api.deepseek.com/v1","model":"deepseek-chat"}}`
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Arbiter.BaseURL != "https://api.deepseek.com/v1" {
			t.Errorf("base_url = %q", cfg.Arbiter.BaseURL)
		}
		if cfg.Arbiter.Model != "deepseek-chat" {
			t.Errorf("model = %q", cfg.Arbiter.Model)
		}
	})

	t.Run("empty arbiter config is valid", func(t *testing.T) {
		data := `{"repos":[{"owner":"a","repo":"b","token":"t"}]}`
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestArbiterAPIKeyEnvVar(t *testing.T) {
	data := `{"repos":[{"owner":"a","repo":"b","token":"t"}],"arbiter":{"base_url":"https://api.deepseek.com/v1","model":"deepseek-chat"}}`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ARBITER_API_KEY", "test-key-123")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Arbiter.APIKey != "test-key-123" {
		t.Errorf("api_key = %q, want %q", cfg.Arbiter.APIKey, "test-key-123")
	}
}
