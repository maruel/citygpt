// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

// Package internal is internal stuff.
package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/maruel/genai/scoreboard"
)

// ListProvider list the available providers.
func ListProvider(ctx context.Context) []string {
	var names []string
	for name, f := range providers.Available(ctx) {
		_, err := f(ctx, &genai.ProviderOptions{Model: genai.ModelNone}, nil)
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LoadProvider loads the first available provider, prioritizing the one requested first.
func LoadProvider(ctx context.Context, provider string, opts *genai.ProviderOptions, wrapper func(http.RoundTripper) http.RoundTripper) (genai.Provider, error) {
	var f func(ctx context.Context, opts *genai.ProviderOptions, wrapper func(http.RoundTripper) http.RoundTripper) (genai.Provider, error)
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
			f = fac
			break
		}
	} else if f = providers.All[provider]; f == nil {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	c, err := f(ctx, opts, wrapper)
	if err != nil {
		return nil, err
	}
	if s, ok := c.(scoreboard.ProviderScore); ok {
		id := c.ModelID()
		for _, sc := range s.Scoreboard().Scenarios {
			if slices.Contains(sc.Models, id) {
				if sc.ThinkingTokenStart != "" {
					c = &adapters.ProviderThinking{Provider: c, ThinkingTokenStart: sc.ThinkingTokenStart, ThinkingTokenEnd: sc.ThinkingTokenEnd}
				}
				break
			}
		}
	}
	return &ProviderLog{c}, nil
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

func (l *ProviderLog) GenSync(ctx context.Context, msgs genai.Messages, opts genai.Options) (genai.Result, error) {
	start := time.Now()
	resp, err := l.Provider.GenSync(ctx, msgs, opts)
	slog.DebugContext(ctx, "GenSync", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err, "usage", resp.Usage)
	return resp, err
}

func (l *ProviderLog) GenStream(ctx context.Context, msgs genai.Messages, replies chan<- genai.ReplyFragment, opts genai.Options) (genai.Result, error) {
	start := time.Now()
	resp, err := l.Provider.GenStream(ctx, msgs, replies, opts)
	slog.DebugContext(ctx, "GenStream", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err)
	return resp, err
}

func (l *ProviderLog) Unwrap() genai.Provider {
	return l.Provider
}
