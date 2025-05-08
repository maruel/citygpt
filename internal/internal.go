// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
	"github.com/maruel/genai/gemini"
	"github.com/maruel/genai/groq"
)

var (
	modelFlag   string
	useCerebras bool
	useGemini   bool
	useGroq     bool
)

func init() {
	if os.Getenv("GEMINI_API_KEY") != "" {
		useGemini = true
		flag.StringVar(&modelFlag, "model", "gemini-2.5-flash-preview-04-17", "Model to use for chat completions")
	} else if os.Getenv("GROQ_API_KEY") != "" {
		useGroq = true
		flag.StringVar(&modelFlag, "model", "meta-llama/llama-4-scout-17b-16e-instruct", "Model to use for chat completions")
	} else if os.Getenv("CEREBRAS_API_KEY") != "" {
		// Cerebras limits to 8K context on free tier.
		useCerebras = true
		// llama3.1-8b
		// llama-3.3-70b
		// llama-4-scout-17b-16e-instruct
		flag.StringVar(&modelFlag, "model", "llama-4-scout-17b-16e-instruct", "Model to use for chat completions")
	}
}

func LoadProvider(ctx context.Context) (genai.ChatProvider, error) {
	if useGemini {
		c, err := gemini.New("", "")
		if err == nil {
			if models, err2 := c.ListModels(ctx); err2 == nil && len(models) > 0 {
				modelNames := make([]string, 0, len(models))
				found := false
				for _, model := range models {
					n := model.GetID()
					if n == modelFlag {
						found = true
						break
					}
					modelNames = append(modelNames, n)
				}
				if !found {
					return nil, fmt.Errorf("bad model. Available models:\n  %s", strings.Join(modelNames, "\n  "))
				}
			}
		}
		if c, err = gemini.New("", modelFlag); err != nil {
			return nil, err
		}
		return &LoggingChatProvider{c}, nil
	}
	if useCerebras {
		c, err := cerebras.New("", "")
		if err == nil {
			if models, err2 := c.ListModels(ctx); err2 == nil && len(models) > 0 {
				modelNames := make([]string, 0, len(models))
				found := false
				for _, model := range models {
					n := model.GetID()
					if n == modelFlag {
						found = true
						break
					}
					modelNames = append(modelNames, n)
				}
				if !found {
					return nil, fmt.Errorf("bad model. Available models:\n  %s", strings.Join(modelNames, "\n  "))
				}
			}
		}
		if c, err = cerebras.New("", modelFlag); err != nil {
			return nil, err
		}
		return &LoggingChatProvider{c}, nil
	}
	if useGroq {
		c, err := groq.New("", "")
		if err == nil {
			if models, err2 := c.ListModels(ctx); err2 == nil && len(models) > 0 {
				modelNames := make([]string, 0, len(models))
				found := false
				for _, model := range models {
					n := model.GetID()
					if n == modelFlag {
						found = true
						break
					}
					modelNames = append(modelNames, n)
				}
				if !found {
					return nil, fmt.Errorf("bad model. Available models:\n  %s", strings.Join(modelNames, "\n  "))
				}
			}
		}
		if c, err = groq.New("", modelFlag); err != nil {
			return nil, err
		}
		return &LoggingChatProvider{c}, nil
	}
	return nil, errors.New("set either CEREBRAS_API_KEY, GEMINI_API_KEY or GROQ_API_KEY")
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

// LoggingChatProvider adds logs to the ChatProvider interface.
type LoggingChatProvider struct {
	genai.ChatProvider
}

func (l *LoggingChatProvider) Chat(ctx context.Context, msgs genai.Messages, opts genai.Validatable) (genai.ChatResult, error) {
	start := time.Now()
	resp, err := l.ChatProvider.Chat(ctx, msgs, opts)
	slog.DebugContext(ctx, "genai", "fn", "Chat", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err)
	return resp, err
}

func (l *LoggingChatProvider) ChatStream(ctx context.Context, msgs genai.Messages, opts genai.Validatable, replies chan<- genai.MessageFragment) error {
	start := time.Now()
	err := l.ChatProvider.ChatStream(ctx, msgs, opts, replies)
	slog.DebugContext(ctx, "genai", "fn", "ChatStream", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err)
	return err
}
