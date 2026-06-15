package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateRepos(t *testing.T) {
	repo := func(owner, r, token string) RepoConfig {
		return RepoConfig{Owner: owner, Repo: r, Token: token}
	}
	appRepo := func(owner, r string) RepoConfig {
		return RepoConfig{Owner: owner, Repo: r, AppID: 1, PrivateKeyPath: "/key.pem", InstallationID: 42}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "no repos", cfg: Config{}, wantErr: true},
		{name: "repo missing owner", cfg: Config{Repos: []RepoConfig{{Repo: "r", Token: "tok"}}}, wantErr: true},
		{name: "repo missing name", cfg: Config{Repos: []RepoConfig{{Owner: "o", Token: "tok"}}}, wantErr: true},
		{name: "token auth accepted", cfg: Config{Repos: []RepoConfig{repo("o", "r", "tok")}}, wantErr: false},
		{name: "per-repo app auth accepted", cfg: Config{Repos: []RepoConfig{appRepo("o", "r")}}, wantErr: false},
		{
			name: "dispatcher app auth with app-level installation_id accepted",
			cfg: Config{
				Repos: []RepoConfig{{Owner: "o", Repo: "r"}},
				Apps:  map[string]AppConfig{"dispatcher": {AppID: 1, PrivateKeyPath: "/key.pem", InstallationID: 42}},
			},
			wantErr: false,
		},
		{
			name: "dispatcher app auth with per-repo installation_id accepted",
			cfg: Config{
				Repos: []RepoConfig{{Owner: "o", Repo: "r", InstallationID: 42}},
				Apps:  map[string]AppConfig{"dispatcher": {AppID: 1, PrivateKeyPath: "/key.pem"}},
			},
			wantErr: false,
		},
		{
			name: "dispatcher app auth with per-repo key path accepted",
			cfg: Config{
				Repos: []RepoConfig{{Owner: "o", Repo: "r", PrivateKeyPath: "/key.pem", InstallationID: 42}},
				Apps:  map[string]AppConfig{"dispatcher": {AppID: 1}},
			},
			wantErr: false,
		},
		{
			name: "dispatcher app auth missing installation_id rejected",
			cfg: Config{
				Repos: []RepoConfig{{Owner: "o", Repo: "r"}},
				Apps:  map[string]AppConfig{"dispatcher": {AppID: 1, PrivateKeyPath: "/key.pem"}},
			},
			wantErr: true,
		},
		{
			name: "dispatcher app auth missing key path rejected",
			cfg: Config{
				Repos: []RepoConfig{{Owner: "o", Repo: "r"}},
				Apps:  map[string]AppConfig{"dispatcher": {AppID: 1, InstallationID: 42}},
			},
			wantErr: true,
		},
		{name: "no auth at all rejected", cfg: Config{Repos: []RepoConfig{{Owner: "o", Repo: "r"}}}, wantErr: true},
		{
			name: "mixed token and dispatcher app auth accepted",
			cfg: Config{
				Repos: []RepoConfig{repo("o", "r1", "tok"), {Owner: "o", Repo: "r2"}},
				Apps:  map[string]AppConfig{"dispatcher": {AppID: 1, PrivateKeyPath: "/key.pem", InstallationID: 42}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateRepos()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestPhaseDuration(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		phase string
		want  time.Duration
	}{
		{name: "default when nothing configured", cfg: Config{}, phase: "coder", want: 15 * time.Minute},
		{
			name:  "uses global max_phase_duration",
			cfg:   Config{MaxPhaseDuration: Duration{20 * time.Minute}},
			phase: "planner",
			want:  20 * time.Minute,
		},
		{
			name: "per-phase override takes precedence",
			cfg: Config{
				MaxPhaseDuration: Duration{15 * time.Minute},
				PhaseDurations:   map[string]Duration{"coder": {30 * time.Minute}},
			},
			phase: "coder",
			want:  30 * time.Minute,
		},
		{
			name: "falls back to global when phase not in map",
			cfg: Config{
				MaxPhaseDuration: Duration{20 * time.Minute},
				PhaseDurations:   map[string]Duration{"coder": {30 * time.Minute}},
			},
			phase: "planner",
			want:  20 * time.Minute,
		},
		{
			name: "zero per-phase duration falls back to global",
			cfg: Config{
				MaxPhaseDuration: Duration{20 * time.Minute},
				PhaseDurations:   map[string]Duration{"coder": {0}},
			},
			phase: "coder",
			want:  20 * time.Minute,
		},
		{
			name:  "zero global falls back to hardcoded default",
			cfg:   Config{MaxPhaseDuration: Duration{0}},
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
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		data := `{"arbiter":{"base_url":"https://api.deepseek.com/v1"}}`
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
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		data := `{"arbiter":{"base_url":"https://api.deepseek.com/v1","model":"deepseek-chat"}}`
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
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestArbiterAPIKeyEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"arbiter":{"base_url":"https://api.deepseek.com/v1","model":"deepseek-chat"}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARBITER_API_KEY", "test-key-123")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Arbiter.APIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want %q", cfg.Arbiter.APIKey, "test-key-123")
	}
}

func TestEvalConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Eval.CasesDir != "tests/golden" {
		t.Errorf("CasesDir = %q, want tests/golden", cfg.Eval.CasesDir)
	}
	if cfg.Eval.TimeoutPerRun.Duration != 5*time.Minute {
		t.Errorf("TimeoutPerRun = %v, want 5m", cfg.Eval.TimeoutPerRun.Duration)
	}
	if cfg.Eval.ResultsDir != "eval-results" {
		t.Errorf("ResultsDir = %q, want eval-results", cfg.Eval.ResultsDir)
	}
}
