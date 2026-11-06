package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/home-operations/external-dns-unifi-webhook/internal/config"
	"github.com/home-operations/external-dns-unifi-webhook/internal/dnsprovider"
	"github.com/home-operations/external-dns-unifi-webhook/internal/metrics"
	"github.com/home-operations/external-dns-unifi-webhook/internal/server"
	"github.com/home-operations/external-dns-unifi-webhook/internal/webhook"
)

var (
	Version = "local"
	Gitsha  = "?"
)

func main() {
	initLogger()
	slog.Info("starting external-dns-unifi-webhook", "version", Version, "gitsha", Gitsha)

	metrics.New(Version)

	cfg, err := config.Init()
	if err != nil {
		slog.Error("loading configuration", "error", err)
		os.Exit(1)
	}

	provider, err := dnsprovider.Init(&cfg)
	if err != nil {
		slog.Error("initializing provider", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT,
	)
	defer stop()

	// /readyz delegates to provider.Records. A failure means that the upstream
	// UniFi controller is unreachable or that the credentials are invalid.
	// Both must keep the pod out of rotation.
	probe := func(ctx context.Context) error {
		_, err := provider.Records(ctx)

		return err
	}

	if err := server.Run(ctx, &cfg, webhook.New(provider), probe); err != nil {
		slog.Error("running server", "error", err)
		os.Exit(1)
	}
}

func initLogger() {
	var level slog.Level
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// AddSource emits the calling function/file/line on every record. That's
	// useful for debugging but doubles the size of routine logs and the same
	// access-log call site appears on every request. Only opt in at debug.
	opts := &slog.HandlerOptions{
		AddSource: level == slog.LevelDebug,
		Level:     level,
	}

	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}
