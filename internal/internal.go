// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
	"github.com/maruel/genai/gemini"
	"github.com/maruel/genai/groq"
)

var (
	modelFlag   string
	useCerebras string
	useGemini   string
	useGroq     string
)

func init() {
	if os.Getenv("GEMINI_API_KEY") != "" {
		useGemini = "gemini-2.5-flash-preview-04-17"
	}
	if os.Getenv("GROQ_API_KEY") != "" {
		useGroq = "meta-llama/llama-4-scout-17b-16e-instruct"
	}
	if os.Getenv("CEREBRAS_API_KEY") != "" {
		// Cerebras limits to 8K context on free tier.
		// llama3.1-8b
		// llama-3.3-70b
		// llama-4-scout-17b-16e-instruct
		// qwen-3-32b
		useCerebras = "qwen-3-32b"
	}
}

// LoadProvider loads the first available provider, prioritizing the one requested first.
func LoadProvider(ctx context.Context, provider, model string) (genai.ChatProvider, error) {
	if provider == "" {
		if useGemini != "" {
			provider = "gemini"
		} else if useGroq != "" {
			provider = "groq"
		} else if useCerebras != "" {
			provider = "cerebras"
		} else {
			return nil, errors.New("no provider available")
		}
	}
	var getClient func(model string) (genai.ChatProvider, error)
	switch provider {
	case "cerebras":
		if model == "" {
			model = useCerebras
		}
		getClient = func(model string) (genai.ChatProvider, error) {
			c, err := cerebras.New("", model)
			if err != nil {
				return c, err
			}
			return &genai.ChatProviderThinking{Provider: c, TagName: "think"}, nil
		}
	case "gemini":
		if model == "" {
			model = useGemini
		}
		getClient = func(model string) (genai.ChatProvider, error) {
			return gemini.New("", model)
		}
	case "groq":
		if model == "" {
			model = useGroq
		}
		getClient = func(model string) (genai.ChatProvider, error) {
			return groq.New("", model)
		}
	default:
		return nil, errors.New("set either CEREBRAS_API_KEY, GEMINI_API_KEY or GROQ_API_KEY")
	}
	return loadProvider(ctx, getClient, model)
}

func loadProvider(ctx context.Context, getClient func(model string) (genai.ChatProvider, error), model string) (genai.ChatProvider, error) {
	/*
		c, err := getClient("")
		if err == nil {
			if models, err2 := c.(genai.ModelProvider).ListModels(ctx); err2 == nil && len(models) > 0 {
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
	*/
	c, err := getClient(model)
	if err != nil {
		return nil, err
	}
	return &ChatProviderLog{c}, nil
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

// ChatProviderLog adds logs to the ChatProvider interface.
type ChatProviderLog struct {
	genai.ChatProvider
}

func (l *ChatProviderLog) Chat(ctx context.Context, msgs genai.Messages, opts genai.Validatable) (genai.ChatResult, error) {
	start := time.Now()
	resp, err := l.ChatProvider.Chat(ctx, msgs, opts)
	slog.DebugContext(ctx, "Chat", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err, "usage", resp.Usage)
	return resp, err
}

func (l *ChatProviderLog) ChatStream(ctx context.Context, msgs genai.Messages, opts genai.Validatable, replies chan<- genai.MessageFragment) (genai.ChatResult, error) {
	start := time.Now()
	resp, err := l.ChatProvider.ChatStream(ctx, msgs, opts, replies)
	slog.DebugContext(ctx, "ChatStream", "msgs", len(msgs), "dur", time.Since(start).Round(time.Millisecond), "err", err)
	return resp, err
}
