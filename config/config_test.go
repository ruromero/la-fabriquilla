package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	t.Run("complete arbiter config loads", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		data := `{
			"default_model": "qwen2.5-coder:14b@ollama",
			"endpoints": {"ollama": {"base_url": "http://localhost:11434/v1"}},
			"arbiter": {"model": "deepseek-chat@deepseek"}
		}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Arbiter.Model != "deepseek-chat@deepseek" {
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
	data := `{
		"default_model": "qwen2.5-coder:14b@ollama",
		"endpoints": {
			"ollama": {"base_url": "http://localhost:11434/v1"},
			"deepseek": {"base_url": "https://api.deepseek.com/v1", "api_key_env": "ARBITER_API_KEY"}
		},
		"arbiter": {"model": "deepseek-chat@deepseek"}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARBITER_API_KEY", "test-key-123")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, apiKey, err := cfg.ResolveModel(cfg.Arbiter.Model)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if apiKey != "test-key-123" {
		t.Errorf("apiKey = %q, want %q", apiKey, "test-key-123")
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

func TestResolveModel(t *testing.T) {
	cfg := Config{
		DefaultModel: "qwen2.5-coder:14b@ollama",
		Endpoints: map[string]EndpointConfig{
			"ollama":   {BaseURL: "http://localhost:11434/v1"},
			"deepseek": {BaseURL: "https://api.deepseek.com/v1", APIKey: "dk-test"},
			"gemini":   {BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKey: "gem-test"},
		},
	}

	tests := []struct {
		name    string
		spec    string
		model   string
		baseURL string
		apiKey  string
		wantErr bool
	}{
		{name: "explicit endpoint", spec: "deepseek-chat@deepseek", model: "deepseek-chat", baseURL: "https://api.deepseek.com/v1", apiKey: "dk-test"},
		{name: "bare name inherits default endpoint", spec: "qwen3:14b", model: "qwen3:14b", baseURL: "http://localhost:11434/v1", apiKey: ""},
		{name: "full default_model spec", spec: "qwen2.5-coder:14b@ollama", model: "qwen2.5-coder:14b", baseURL: "http://localhost:11434/v1", apiKey: ""},
		{name: "unknown endpoint errors", spec: "model@nonexistent", wantErr: true},
		{name: "empty spec errors", spec: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, baseURL, apiKey, err := cfg.ResolveModel(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if model != tt.model {
				t.Errorf("model = %q, want %q", model, tt.model)
			}
			if baseURL != tt.baseURL {
				t.Errorf("baseURL = %q, want %q", baseURL, tt.baseURL)
			}
			if apiKey != tt.apiKey {
				t.Errorf("apiKey = %q, want %q", apiKey, tt.apiKey)
			}
		})
	}
}

func TestResolveModelBareDefaultErrors(t *testing.T) {
	cfg := Config{
		DefaultModel: "qwen3:14b",
		Endpoints:    map[string]EndpointConfig{"ollama": {BaseURL: "http://localhost:11434/v1"}},
	}
	_, _, _, err := cfg.ResolveModel("some-model")
	if err == nil {
		t.Fatal("expected error when default_model has no @endpoint and spec is bare")
	}
}

func TestLoadConfigMigratesLegacyInference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"inference": {"base_url": "http://localhost:11434/v1", "model": "qwen3:14b"},
		"planner": {"base_url": "https://generativelanguage.googleapis.com/v1beta/openai", "model": "gemini-2.5-flash"},
		"arbiter": {"base_url": "https://api.deepseek.com/v1", "model": "deepseek-chat"}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFERENCE_API_KEY", "inf-key")
	t.Setenv("PLANNER_API_KEY", "plan-key")
	t.Setenv("ARBITER_API_KEY", "arb-key")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DefaultModel == "" {
		t.Fatal("DefaultModel not migrated")
	}
	model, baseURL, apiKey, err := cfg.ResolveModel(cfg.DefaultModel)
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if model != "qwen3:14b" {
		t.Errorf("default model = %q, want qwen3:14b", model)
	}
	if baseURL != "http://localhost:11434/v1" {
		t.Errorf("default baseURL = %q", baseURL)
	}
	if apiKey != "inf-key" {
		t.Errorf("default apiKey = %q", apiKey)
	}

	model, baseURL, apiKey, err = cfg.ResolveModel(cfg.Planner.Model)
	if err != nil {
		t.Fatalf("resolve planner: %v", err)
	}
	if model != "gemini-2.5-flash" {
		t.Errorf("planner model = %q", model)
	}
	if apiKey != "plan-key" {
		t.Errorf("planner apiKey = %q", apiKey)
	}
	_ = baseURL

	model, _, apiKey, err = cfg.ResolveModel(cfg.Arbiter.Model)
	if err != nil {
		t.Fatalf("resolve arbiter: %v", err)
	}
	if model != "deepseek-chat" {
		t.Errorf("arbiter model = %q", model)
	}
	if apiKey != "arb-key" {
		t.Errorf("arbiter apiKey = %q", apiKey)
	}
}

func TestLoadConfigMigratesGeminiAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"inference": {"base_url": "http://localhost:11434/v1", "model": "qwen3:14b"}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEMINI_API_KEY", "gem-key")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Researcher.Model == "" {
		t.Fatal("Researcher.Model not migrated")
	}
	_, baseURL, apiKey, err := cfg.ResolveModel(cfg.Researcher.Model)
	if err != nil {
		t.Fatalf("resolve researcher: %v", err)
	}
	if baseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Errorf("researcher baseURL = %q", baseURL)
	}
	if apiKey != "gem-key" {
		t.Errorf("researcher apiKey = %q", apiKey)
	}
}

func TestLoadConfigNewSchemaPassthrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"default_model": "qwen2.5-coder:14b@ollama",
		"planner": {"model": "gemini-2.5-flash@gemini"},
		"arbiter": {"model": "deepseek-chat@deepseek"},
		"endpoints": {
			"ollama": {"base_url": "http://localhost:11434/v1"},
			"deepseek": {"base_url": "https://api.deepseek.com/v1", "api_key_env": "DEEPSEEK_API_KEY"},
			"gemini": {"base_url": "https://generativelanguage.googleapis.com/v1beta/openai", "api_key_env": "GEMINI_API_KEY"}
		}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "dk-123")
	t.Setenv("GEMINI_API_KEY", "gem-456")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DefaultModel != "qwen2.5-coder:14b@ollama" {
		t.Errorf("DefaultModel = %q", cfg.DefaultModel)
	}
	model, baseURL, apiKey, err := cfg.ResolveModel(cfg.Arbiter.Model)
	if err != nil {
		t.Fatalf("resolve arbiter: %v", err)
	}
	if model != "deepseek-chat" || baseURL != "https://api.deepseek.com/v1" || apiKey != "dk-123" {
		t.Errorf("arbiter got model=%q url=%q key=%q", model, baseURL, apiKey)
	}
}
