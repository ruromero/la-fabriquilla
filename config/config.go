package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	DefaultModel     string                    `json:"default_model,omitempty"`
	Planner          RoleConfig                `json:"planner"`
	Researcher       RoleConfig                `json:"researcher,omitempty"`
	PollInterval     Duration                  `json:"poll_interval"`
	MaxIterations    int                       `json:"max_iterations"`
	MaxCostBudget    int                       `json:"max_cost_budget"`
	MaxPhaseDuration Duration                  `json:"max_phase_duration"`
	MaxPhaseRetries  int                       `json:"max_phase_retries"`
	PhaseDurations   map[string]Duration       `json:"phase_durations,omitempty"`
	ShadowMode       bool                      `json:"shadow_mode"`
	MaxFilesChanged  int                       `json:"max_files_changed"`
	MaxPRSizeLines   int                       `json:"max_pr_size_lines"`
	MaxIssuesPerHour int                       `json:"max_issues_per_hour"`
	MaxIssuesPerDay  int                       `json:"max_issues_per_day"`
	BlockedPaths     []string                  `json:"blocked_paths,omitempty"`
	Serena           SerenaConfig              `json:"serena"`
	Sandbox          SandboxConfig             `json:"sandbox,omitempty"`
	Security         SecurityConfig            `json:"security,omitempty"`
	Arbiter          RoleConfig                `json:"arbiter,omitempty"`
	Endpoints        map[string]EndpointConfig `json:"endpoints,omitempty"`
	Eval             EvalConfig                `json:"eval,omitempty"`
	Repos            []RepoConfig              `json:"repos"`
	Apps             map[string]AppConfig      `json:"apps,omitempty"`
	StateDir         string                    `json:"state_dir,omitempty"`
}

// EndpointConfig defines a named inference endpoint for use with
// eval-runner's model@endpoint syntax.
type EndpointConfig struct {
	BaseURL              string  `json:"base_url"`
	APIKeyEnv            string  `json:"api_key_env,omitempty"`
	APIKey               string  `json:"-"`
	InputPricePerMToken  float64 `json:"input_price_per_million_tokens,omitempty"`
	OutputPricePerMToken float64 `json:"output_price_per_million_tokens,omitempty"`
}

// RoleConfig identifies a model for a pipeline role.
type RoleConfig struct {
	Model string `json:"model"`
}

// SandboxConfig holds settings for OpenShell sandbox execution.
type SandboxConfig struct {
	Enabled   bool   `json:"enabled"`
	PolicyDir string `json:"policy_dir,omitempty"`
	Image     string `json:"image,omitempty"`
}

type AppConfig struct {
	AppID          int64  `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
}

// SecurityConfig holds security-related settings.
type SecurityConfig struct {
	AllowPrivateURLs bool `json:"allow_private_urls"`
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

// ResolveModel parses a "name@endpoint" spec and resolves the endpoint.
// Bare names (no @) inherit the endpoint from DefaultModel.
func (c *Config) ResolveModel(spec string) (model, baseURL, apiKey string, err error) {
	if spec == "" {
		return "", "", "", fmt.Errorf("empty model spec")
	}
	name, epName, hasEndpoint := strings.Cut(spec, "@")
	if !hasEndpoint {
		_, defaultEP, hasDefault := strings.Cut(c.DefaultModel, "@")
		if !hasDefault || defaultEP == "" {
			return "", "", "", fmt.Errorf("model %q has no @endpoint and default_model has no endpoint to inherit", spec)
		}
		epName = defaultEP
	}
	ep, ok := c.Endpoints[epName]
	if !ok {
		return "", "", "", fmt.Errorf("unknown endpoint %q (from model spec %q)", epName, spec)
	}
	return name, ep.BaseURL, ep.APIKey, nil
}

// EvalConfig holds settings for the golden-set eval runner.
type EvalConfig struct {
	CasesDir      string   `json:"cases_dir"`
	RunsPerCase   int      `json:"runs_per_case"`
	TimeoutPerRun Duration `json:"timeout_per_run"`
	ResultsDir    string   `json:"results_dir"`
}

type RepoConfig struct {
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Language       string `json:"language,omitempty"`
	SandboxImage   string `json:"sandbox_image,omitempty"`
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

type legacyConfig struct {
	Inference struct {
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
	} `json:"inference"`
	GeminiAPIKey string `json:"gemini_api_key"`
	Planner      struct {
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
	} `json:"planner"`
	Arbiter struct {
		BaseURL string `json:"base_url"`
		Model   string `json:"model"`
	} `json:"arbiter"`
}

func findOrCreateEndpoint(endpoints map[string]EndpointConfig, baseURL, preferredName, apiKey string) string {
	for name, ep := range endpoints {
		if ep.BaseURL == baseURL {
			if apiKey != "" && ep.APIKey == "" {
				ep.APIKey = apiKey
				endpoints[name] = ep
			}
			return name
		}
	}
	endpoints[preferredName] = EndpointConfig{BaseURL: baseURL, APIKey: apiKey}
	return preferredName
}

func migrateConfig(cfg *Config, data []byte) {
	var legacy legacyConfig
	_ = json.Unmarshal(data, &legacy)

	if cfg.DefaultModel != "" {
		return
	}

	if legacy.Inference.BaseURL == "" {
		return
	}

	if cfg.Endpoints == nil {
		cfg.Endpoints = make(map[string]EndpointConfig)
	}

	infKey := os.Getenv("INFERENCE_API_KEY")
	epName := findOrCreateEndpoint(cfg.Endpoints, legacy.Inference.BaseURL, "ollama", infKey)
	model := legacy.Inference.Model
	if model == "" {
		model = "qwen2.5-coder:14b"
	}
	cfg.DefaultModel = model + "@" + epName

	if legacy.Planner.BaseURL != "" {
		planKey := os.Getenv("PLANNER_API_KEY")
		planEP := findOrCreateEndpoint(cfg.Endpoints, legacy.Planner.BaseURL, "planner-ep", planKey)
		if legacy.Planner.Model != "" {
			cfg.Planner = RoleConfig{Model: legacy.Planner.Model + "@" + planEP}
		}
	}

	if legacy.Arbiter.BaseURL != "" {
		arbKey := os.Getenv("ARBITER_API_KEY")
		arbEP := findOrCreateEndpoint(cfg.Endpoints, legacy.Arbiter.BaseURL, "arbiter-ep", arbKey)
		if legacy.Arbiter.Model != "" {
			cfg.Arbiter = RoleConfig{Model: legacy.Arbiter.Model + "@" + arbEP}
		}
	}

	if legacy.GeminiAPIKey != "" || os.Getenv("GEMINI_API_KEY") != "" {
		gemKey := legacy.GeminiAPIKey
		if v := os.Getenv("GEMINI_API_KEY"); v != "" {
			gemKey = v
		}
		gemURL := "https://generativelanguage.googleapis.com/v1beta/openai"
		gemEP := findOrCreateEndpoint(cfg.Endpoints, gemURL, "gemini", gemKey)
		if cfg.Researcher.Model == "" {
			cfg.Researcher = RoleConfig{Model: "gemini-2.5-flash@" + gemEP}
		}
	}
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{
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
		Eval: EvalConfig{
			CasesDir:      "tests/golden",
			TimeoutPerRun: Duration{5 * time.Minute},
			ResultsDir:    "eval-results",
		},
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	migrateConfig(&cfg, data)

	for name, ep := range cfg.Endpoints {
		if ep.APIKeyEnv != "" {
			if v := os.Getenv(ep.APIKeyEnv); v != "" {
				ep.APIKey = v
				cfg.Endpoints[name] = ep
			}
		}
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

	if cfg.Sandbox.Enabled {
		if cfg.Sandbox.PolicyDir == "" {
			return Config{}, fmt.Errorf("sandbox.policy_dir is required when sandbox is enabled")
		}
		if cfg.Sandbox.Image == "" {
			return Config{}, fmt.Errorf("sandbox.image is required when sandbox is enabled")
		}
	}

	return cfg, nil
}

// ValidateRepos checks that at least one repo is configured and that every
// repo has valid auth. Called by the dispatcher; other binaries (eval-runner,
// phase agents) do not require repos.
//
// A repo is considered authenticated if any of the following is true:
//   - r.Token is set
//   - r.UsesAppAuth() (repo-level app_id + private_key_path + installation_id)
//   - cfg.Apps["dispatcher"] has AppID and a PrivateKeyPath (from the app or
//     the repo entry), and an InstallationID (from the app or the repo entry)
func (c Config) ValidateRepos() error {
	if len(c.Repos) == 0 {
		return fmt.Errorf("no repos configured")
	}
	dispApp := c.Apps["dispatcher"]
	dispHasKey := dispApp.AppID != 0 && dispApp.PrivateKeyPath != ""
	for i, r := range c.Repos {
		if r.Owner == "" || r.Repo == "" {
			return fmt.Errorf("repo %d: owner and repo are required", i)
		}
		if r.Token != "" || r.UsesAppAuth() {
			continue
		}
		// Accept shared dispatcher app auth: AppID + key path (app or repo) +
		// installation ID (app or repo), mirroring NewGitHubClientForApp logic.
		hasKey := dispHasKey || (dispApp.AppID != 0 && r.PrivateKeyPath != "")
		hasInstall := dispApp.InstallationID != 0 || r.InstallationID != 0
		if hasKey && hasInstall {
			continue
		}
		return fmt.Errorf("repo %d (%s/%s): requires token, per-repo app auth, or apps.dispatcher with app_id, private_key_path, and installation_id", i, r.Owner, r.Repo)
	}
	return nil
}
