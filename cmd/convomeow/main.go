package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/notborges/convomeow/internal/api/native"
	"github.com/notborges/convomeow/internal/app"
	"github.com/notborges/convomeow/internal/cli"
	"github.com/notborges/convomeow/internal/config"
	"github.com/notborges/convomeow/internal/media"
	"github.com/notborges/convomeow/internal/providers/whatsapp"
	"github.com/notborges/convomeow/internal/store/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("convomeow", flag.ContinueOnError)
	dataDir := flags.String("data-dir", envOr("CONVOMEOW_DATA_DIR", "./data"), "data directory")
	listen := flags.String("listen", envOr("CONVOMEOW_LISTEN", "127.0.0.1:8787"), "HTTP listen address for serve")
	baseURL := flags.String("url", envOr("CONVOMEOW_URL", "http://127.0.0.1:8787"), "daemon URL for CLI commands")
	configPath := flags.String("config", "", "configuration file for serve (default: <data-dir>/config.yaml)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		return errors.New("expected serve, account, chat, or message command")
	}
	paths, err := config.DataPaths(*dataDir)
	if err != nil {
		return err
	}
	if remaining[0] == "serve" {
		if len(remaining) != 1 {
			return errors.New("serve takes no arguments")
		}
		if *configPath == "" {
			*configPath = filepath.Join(paths.Dir, "config.yaml")
		}
		return serve(paths, *listen, *configPath)
	}
	token, err := paths.ReadToken()
	if err != nil {
		return fmt.Errorf("read control token (start convomeow serve first): %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return cli.New(*baseURL, token).Run(ctx, remaining)
}

func serve(paths config.Paths, listen, configPath string) error {
	if err := paths.EnsureDir(); err != nil {
		return err
	}
	lock := flock.New(paths.ProcessLock)
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("another service is using %s", paths.Dir)
	}
	defer lock.Unlock()
	token, err := paths.CreateOrReadToken()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	mediaConfig, err := config.LoadMedia(configPath, paths.Dir)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	stores := make(map[string]media.Store, len(mediaConfig.Profiles))
	for id, profile := range mediaConfig.Profiles {
		switch profile.Driver {
		case "local":
			stores[id], err = media.NewLocal(profile.Path)
		case "s3":
			stores[id], err = media.NewS3(ctx, media.S3Options{Bucket: profile.Bucket, Region: profile.Region,
				Endpoint: profile.Endpoint, AccessKey: profile.AccessKey, SecretKey: profile.SecretKey})
		}
		if err != nil {
			return fmt.Errorf("open media profile %q: %w", id, err)
		}
	}
	registry, err := media.NewRegistry(mediaConfig.ActiveProfile, stores)
	if err != nil {
		return err
	}
	repo, err := sqlite.Open(ctx, paths.AppDB)
	if err != nil {
		return fmt.Errorf("open app database: %w", err)
	}
	connector, err := whatsapp.Open(ctx, paths.WhatsAppDB)
	if err != nil {
		repo.Close()
		return fmt.Errorf("open WhatsApp database: %w", err)
	}
	service := app.NewWithMedia(repo, connector, slog.Default(), app.MediaOptions{Stores: registry,
		TempDir: filepath.Join(paths.Dir, "media-tmp"), MaxFileBytes: mediaConfig.MaxFileBytes,
		MaxTotalBytes: mediaConfig.MaxTotalBytes, MaxTempBytes: mediaConfig.MaxTempBytes, Workers: mediaConfig.Workers})
	if err := service.Start(ctx); err != nil {
		service.Close()
		return fmt.Errorf("start accounts: %w", err)
	}
	server := &http.Server{Addr: listen, Handler: native.New(service, token), ReadHeaderTimeout: 10 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	slog.Info("service started", "listen", listen, "data_dir", paths.Dir, "accounts", len(service.ListAccounts()))
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			service.Close()
			return err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	serviceErr := service.Close()
	return errors.Join(shutdownErr, serviceErr)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
