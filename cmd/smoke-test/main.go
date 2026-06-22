package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "full-mock", "execution mode: full, mock-github, full-mock")
	timeout := flag.Duration("timeout", 30*time.Minute, "maximum test duration")
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	slog.Info("smoke test starting", "mode", *mode, "timeout", timeout.String())

	var err error
	switch *mode {
	case "full-mock":
		err = runFullMock(ctx)
	case "mock-github":
		err = runMockGitHub(ctx, *configPath)
	case "full":
		err = runFull(ctx, *configPath)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}

	if err != nil {
		slog.Error("smoke test FAILED", "error", err)
		os.Exit(1)
	}
	slog.Info("smoke test PASSED")
}
