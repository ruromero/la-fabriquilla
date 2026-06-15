package config

import (
	"testing"
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
