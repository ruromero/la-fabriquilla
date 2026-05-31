package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	OllamaURL        string               `json:"ollama_url"`
	GeminiAPIKey     string               `json:"gemini_api_key,omitempty"`
	Planner          PlannerConfig        `json:"planner"`
	PollInterval     Duration             `json:"poll_interval"`
	MaxIterations    int                  `json:"max_iterations"`
	MaxCostBudget    int                  `json:"max_cost_budget"`
	MaxPhaseDuration Duration             `json:"max_phase_duration"`
	MaxPhaseRetries  int                  `json:"max_phase_retries"`
	PhaseDurations   map[string]Duration  `json:"phase_durations,omitempty"`
	ShadowMode       bool                 `json:"shadow_mode"`
	MaxFilesChanged  int                  `json:"max_files_changed"`
	MaxPRSizeLines   int                  `json:"max_pr_size_lines"`
	MaxIssuesPerHour int                  `json:"max_issues_per_hour"`
	MaxIssuesPerDay  int                  `json:"max_issues_per_day"`
	BlockedPaths     []string             `json:"blocked_paths,omitempty"`
	Serena           SerenaConfig         `json:"serena"`
	Repos            []RepoConfig         `json:"repos"`
	Apps             map[string]AppConfig `json:"apps,omitempty"`
	StateDir         string               `json:"state_dir,omitempty"`
}

type AppConfig struct {
	AppID          int64  `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
}

type PlannerConfig struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key,omitempty"`
}

type SerenaConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func (s SerenaConfig) Enabled() bool {
	return s.Command != ""
}

// PhaseDuration returns the timeout for the given phase binary.
// It checks per-phase overrides first, then the global default,
// falling back to 15 minutes if nothing is configured.
func (c *Config) PhaseDuration(phase string) time.Duration {
	if d, ok := c.PhaseDurations[phase]; ok && d.Duration > 0 {
		return d.Duration
	}
	if c.MaxPhaseDuration.Duration > 0 {
		return c.MaxPhaseDuration.Duration
	}
	return 15 * time.Minute
}

type RepoConfig struct {
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Token          string `json:"token,omitempty"`
	AppID          int64  `json:"app_id,omitempty"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
	InstallationID int64  `json:"installation_id,omitempty"`
}

func (r RepoConfig) UsesAppAuth() bool {
	return r.AppID != 0 && r.PrivateKeyPath != "" && r.InstallationID != 0
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{
		OllamaURL:        "http://ollama.ai.svc.cluster.local:11434",
		PollInterval:     Duration{30 * time.Second},
		MaxIterations:    3,
		MaxCostBudget:    100000,
		MaxPhaseDuration: Duration{15 * time.Minute},
		MaxPhaseRetries:  2,
		ShadowMode:       true,
		MaxFilesChanged:  20,
		MaxPRSizeLines:   500,
		MaxIssuesPerHour: 5,
		MaxIssuesPerDay:  20,
		BlockedPaths: []string{
			".github/workflows/*",
			"CODEOWNERS",
			".pr_agent.toml",
			"CONVENTIONS.md",
			"ARCHITECTURE.md",
			"CLAUDE.md",
			".serena/*",
			"deploy/*",
		},
		StateDir: "/data/pipeline",
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.GeminiAPIKey = v
	}

	if v := os.Getenv("PLANNER_API_KEY"); v != "" {
		cfg.Planner.APIKey = v
	}

	if v := os.Getenv("PIPELINE_STATE_DIR"); v != "" {
		cfg.StateDir = v
	}

	globalKeyPath := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH")
	if globalKeyPath != "" {
		for i := range cfg.Repos {
			if cfg.Repos[i].PrivateKeyPath == "" {
				cfg.Repos[i].PrivateKeyPath = globalKeyPath
			}
		}
	}
	appKeyEnvs := map[string]string{
		"dispatcher": "FABRIQUILLA_DISPATCHER_KEY_PATH",
		"worker":     "FABRIQUILLA_WORKER_KEY_PATH",
		"committer":  "FABRIQUILLA_COMMITTER_KEY_PATH",
	}
	for role, envVar := range appKeyEnvs {
		if v := os.Getenv(envVar); v != "" {
			if cfg.Apps == nil {
				cfg.Apps = make(map[string]AppConfig)
			}
			app := cfg.Apps[role]
			app.PrivateKeyPath = v
			cfg.Apps[role] = app
		}
	}
	if globalKeyPath != "" {
		for role, app := range cfg.Apps {
			if app.PrivateKeyPath == "" {
				app.PrivateKeyPath = globalKeyPath
				cfg.Apps[role] = app
			}
		}
	}

	if len(cfg.Repos) == 0 {
		return Config{}, fmt.Errorf("no repos configured")
	}

	for i, r := range cfg.Repos {
		if r.Owner == "" || r.Repo == "" {
			return Config{}, fmt.Errorf("repo %d: owner and repo are required", i)
		}
		if !r.UsesAppAuth() && r.Token == "" {
			return Config{}, fmt.Errorf("repo %d: either token or app auth (app_id, private_key_path, installation_id) required", i)
		}
	}

	return cfg, nil
}
