// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maruel/citygpt/internal/htmlparse"
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
		return gemini.New("", modelFlag)
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
		return cerebras.New("", modelFlag)
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
		return groq.New("", modelFlag)
	}
	return nil, errors.New("set either CEREBRAS_API_KEY or GROQ_API_KEY")
}

const summarizationPrompt = "You are a helpful assistant that summarizes text content accurately and concisely. Do not mention what you are doing or your constraints. Do not mention the city or the fact it is about by-laws. Please summarize the subject of following text as a single long line:"

// ProcessHTML from a single URL and saves it
func ProcessHTML(ctx context.Context, c genai.ChatProvider, r io.Reader, md, outputDir string) (string, string, error) {
	textContent, pageTitle, err := htmlparse.ExtractTextFromHTML(r)
	if err != nil {
		return pageTitle, "", fmt.Errorf("failed to extract text: %w", err)
	}
	filePath := filepath.Join(outputDir, md)
	if err = os.WriteFile(filePath, []byte(textContent), 0o644); err != nil {
		return pageTitle, "", fmt.Errorf("failed to write file %s: %w", filePath, err)
	}
	messages := genai.Messages{
		genai.NewTextMessage(genai.User, summarizationPrompt),
		genai.NewTextMessage(genai.User, textContent),
	}
	opts := genai.ChatOptions{Seed: 1, Temperature: 0.3, MaxTokens: 1024 * 1024}
	resp, err := c.Chat(ctx, messages, &opts)
	if err != nil {
		return pageTitle, "", err
	}
	return pageTitle, resp.Contents[0].Text, nil
}

// Index is the content of index.json.
type Index struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Items   []Item    `json:"items"`
}

// Item is one indexed item.
type Item struct {
	URL     string `json:"url"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
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
