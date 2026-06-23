package harness

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ruromero/la-fabriquilla/config"
	"github.com/ruromero/la-fabriquilla/github"
	"github.com/ruromero/la-fabriquilla/mcp"
)

type SerenaSession struct {
	CloneDir string
	Client   *mcp.Client
	Cleanup  func()
}

func CloneAndStartSerena(ctx context.Context, gh github.Service, cfg config.SerenaConfig) (*SerenaSession, error) {
	if !cfg.Enabled() {
		return nil, nil
	}

	cloneDir, cloneCleanup, err := gh.CloneShallow(ctx)
	if err != nil {
		return nil, fmt.Errorf("clone repo: %w", err)
	}

	sess, err := startSerenaInDir(ctx, cloneDir, cfg)
	if err != nil {
		cloneCleanup()
		return nil, err
	}

	sess.Cleanup = func() {
		if err := sess.Client.Stop(); err != nil {
			slog.Warn("failed to stop Serena", "error", err)
		}
		cloneCleanup()
	}

	return sess, nil
}

func StartSerenaFromClone(ctx context.Context, cloneDir string, cfg config.SerenaConfig) (*SerenaSession, error) {
	if !cfg.Enabled() {
		return nil, nil
	}

	sess, err := startSerenaInDir(ctx, cloneDir, cfg)
	if err != nil {
		return nil, err
	}

	sess.Cleanup = func() {
		if err := sess.Client.Stop(); err != nil {
			slog.Warn("failed to stop Serena", "error", err)
		}
	}

	return sess, nil
}

func startSerenaInDir(ctx context.Context, cloneDir string, cfg config.SerenaConfig) (*SerenaSession, error) {
	lspBinDir, err := InstallLanguageServers(ctx, cloneDir)
	if err != nil {
		slog.Warn("failed to set up language servers", "error", err)
	}

	args := make([]string, len(cfg.Args), len(cfg.Args)+2)
	copy(args, cfg.Args)
	args = append(args, "--project", cloneDir)
	client := mcp.NewClient(cfg.Command, args...)
	if lspBinDir != "" {
		env := os.Environ()
		npmBin := fmt.Sprintf("%s/bin", lspBinDir)
		env = append(env, fmt.Sprintf("PATH=%s:%s:%s", lspBinDir, npmBin, os.Getenv("PATH")))
		client.SetEnv(env)
	}
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("start Serena: %w", err)
	}

	return &SerenaSession{
		CloneDir: cloneDir,
		Client:   client,
	}, nil
}
