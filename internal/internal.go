// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

// Package internal is internal stuff.
package internal

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/maruel/genai"
	"github.com/maruel/genai/adapters"
	"github.com/maruel/genai/providers"
)

// ListProvider list the available providers.
func ListProvider(ctx context.Context) []string {
	return slices.Sorted(maps.Keys(providers.Available(ctx)))
}

// LoadProvider loads the first available provider, prioritizing the one requested first.
func LoadProvider(ctx context.Context, provider string, opts *genai.ProviderOptions, wrapper func(http.RoundTripper) http.RoundTripper) (genai.Provider, error) {
	var cfg providers.Config
	if provider == "" {
		avail := providers.Available(ctx)
		if len(avail) == 0 {
			return nil, errors.New("no provider available; please set environment variables or specify a provider and API keys or remote URL")
		}
		if len(avail) > 1 {
			names := make([]string, 0, len(avail))
			for name := range avail {
				names = append(names, name)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("multiple providers available, select one of: %s", strings.Join(names, ", "))
		}
		for _, fac := range avail {
			cfg = fac
			break
		}
	} else if cfg = providers.All[provider]; cfg.Factory == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := cfg.Factory(ctx, opts, wrapper)
	if err != nil {
		return nil, err
	}
	return &ProviderLog{adapters.WrapReasoning(c)}, nil
}

// GetConfigDir returns the appropriate configuration directory based on the OS
func GetConfigDir() (string, error) {
	// Check if XDG_CONFIG_HOME is set (Linux/Unix)
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return configHome, nil
	}
	// For Windows, use %APPDATA%\.
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return appData, nil
		}
	}
	// Default to ~/.config/
	current, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	return filepath.Join(current.HomeDir, ".config"), nil
}

// ProviderLog adds logs to the Provider interface.
type ProviderLog struct {
	genai.Provider
}

func (l *ProviderLog) GenSync(ctx context.Context, msgs genai.Messages, opts ...genai.Options) (genai.Result, error) {
	start := time.Now()
	resp, err := l.Provider.GenSync(ctx, msgs, opts...)
	slog.DebugContext(ctx, "GenSync", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err, "usage", resp.Usage)
	return resp, err
}

func (l *ProviderLog) GenStream(ctx context.Context, msgs genai.Messages, opts ...genai.Options) (iter.Seq[genai.ReplyFragment], func() (genai.Result, error)) {
	start := time.Now()
	fragments, finish := l.Provider.GenStream(ctx, msgs, opts...)
	return fragments, func() (genai.Result, error) {
		res, err := finish()
		slog.DebugContext(ctx, "GenStream", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err, "usage", res.Usage)
		return res, err
	}
}

func (l *ProviderLog) Unwrap() genai.Provider {
	return l.Provider
}
