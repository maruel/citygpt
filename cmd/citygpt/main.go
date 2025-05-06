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
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/citygpt/data/ottawa"
	"github.com/maruel/citygpt/internal"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

func watchExecutable(ctx context.Context, cancel context.CancelFunc) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	initialStat, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("failed to stat executable: %w", err)
	}
	initialModTime := initialStat.ModTime()
	initialSize := initialStat.Size()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			currentStat, err := os.Stat(exePath)
			if err != nil {
				slog.WarnContext(ctx, "citygpt", "msg", "Could not stat executable", "err", err)
				continue
			}
			currentModTime := currentStat.ModTime()
			currentSize := currentStat.Size()
			if !currentModTime.Equal(initialModTime) || currentSize != initialSize {
				slog.InfoContext(ctx, "citygpt", "msg", "Executable file was modified, initiating shutdown...")
				cancel()
				break
			}
		}
	}()
	return nil
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	Level := &slog.LevelVar{}
	Level.Set(slog.LevelDebug)
	logger := slog.New(tint.NewHandler(colorable.NewColorable(os.Stderr), &tint.Options{
		Level:      Level,
		TimeFormat: "15:04:05.000", // Like time.TimeOnly plus milliseconds.
		NoColor:    !isatty.IsTerminal(os.Stderr.Fd()),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch t := a.Value.Any().(type) {
			case string:
				if t == "" {
					return slog.Attr{}
				}
			case bool:
				if !t {
					return slog.Attr{}
				}
			case uint64:
				if t == 0 {
					return slog.Attr{}
				}
			case int64:
				if t == 0 {
					return slog.Attr{}
				}
			case float64:
				if t == 0 {
					return slog.Attr{}
				}
			case time.Time:
				if t.IsZero() {
					return slog.Attr{}
				}
			case time.Duration:
				if t == 0 {
					return slog.Attr{}
				}
			}
			return a
		},
	}))
	slog.SetDefault(logger)
	if err := watchExecutable(ctx, cancel); err != nil {
		return err
	}

	appName := flag.String("app-name", "OttawaGPT", "The name of the application displayed in the UI")
	port := flag.String("port", "8080", "The port to run the server on")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unsupported argument")
	}
	c, err := internal.LoadProvider(ctx)
	if err != nil {
		return err
	}
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
