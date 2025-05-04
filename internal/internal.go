// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
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
	Summary string `json:"summary"`
}
