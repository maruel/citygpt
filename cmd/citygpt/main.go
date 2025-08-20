// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lmittmann/tint"
	"github.com/maruel/citygpt/data/ottawa"
	"github.com/maruel/citygpt/internal"
	"github.com/maruel/genai"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

func watchExecutable(ctx context.Context, cancel context.CancelFunc) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	go func() {
		defer w.Close()
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				// Detect writes or chmod events which may indicate a modification
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Chmod) {
					slog.InfoContext(ctx, "citygpt", "msg", "Executable file was modified, initiating shutdown...")
					cancel()
					return
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.WarnContext(ctx, "citygpt", "msg", "Error watching executable", "err", err)
			case <-ctx.Done():
				return
			}
		}
	}()
	if err := w.Add(exePath); err != nil {
		return fmt.Errorf("failed to watch executable: %w", err)
	}
	return nil
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	Level := &slog.LevelVar{}
	Level.Set(slog.LevelInfo)
	logger := slog.New(tint.NewHandler(colorable.NewColorable(os.Stderr), &tint.Options{
		Level:      Level,
		TimeFormat: "15:04:05.000", // Like time.TimeOnly plus milliseconds.
		NoColor:    !isatty.IsTerminal(os.Stderr.Fd()),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			val := a.Value.Any()
			skip := false
			switch t := val.(type) {
			case string:
				skip = t == ""
			case bool:
				skip = !t
			case uint64:
				skip = t == 0
			case int64:
				skip = t == 0
			case float64:
				skip = t == 0
			case time.Time:
				skip = t.IsZero()
			case time.Duration:
				skip = t == 0
			case nil:
				skip = true
			}
			if skip {
				return slog.Attr{}
			}
			return a
		},
	}))
	slog.SetDefault(logger)
	if err := watchExecutable(ctx, cancel); err != nil {
		return err
	}
	names := internal.ListProvider()

	appName := flag.String("app-name", "OttawaGPT", "The name of the application displayed in the UI")
	port := flag.String("port", "8080", "The port to run the server on")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	remote := flag.String("remote", "", "URL to use, useful for local backend")
	model := flag.String("model", "", "model to use, defaults to a good model; use either the model ID or PREFERRED_CHEAP and PREFERRED_SOTA to automatically select cheaper or better models")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unsupported argument")
	}
	if *verbose {
		Level.Set(slog.LevelDebug)
	}
	c, err := internal.LoadProvider(ctx, *provider, &genai.ProviderOptions{Remote: *remote, Model: *model}, nil)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "citygpt", "msg", "Starting server", "app-name", *appName, "port", *port, "provider", *provider, "model", c.ModelID())
	s, err := newServer(ctx, c, *appName, ottawa.DataFS)
	if err != nil {
		return err
	}
	return s.start(ctx, *port)
}

func main() {
	if err := mainImpl(); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "citygpt: %s\n", err)
		os.Exit(1)
	}
}
