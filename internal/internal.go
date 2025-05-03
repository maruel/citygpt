// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package internal

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
	"github.com/maruel/genai/groq"
)

var modelFlag string

var useCerebras bool

func init() {
	if os.Getenv("CEREBRAS_API_KEY") != "" {
		useCerebras = true
		// llama3.1-8b
		// llama-3.3-70b
		// llama-4-scout-17b-16e-instruct
		flag.StringVar(&modelFlag, "model", "llama-4-scout-17b-16e-instruct", "Model to use for chat completions")
	} else if os.Getenv("GROQ_API_KEY") != "" {
		flag.StringVar(&modelFlag, "model", "meta-llama/llama-4-scout-17b-16e-instruct", "Model to use for chat completions")
	} else {
		// TODO: Gross hack.
		fmt.Fprintf(os.Stderr, "Set either CEREBRAS_API_KEY or GROQ_API_KEY")
		os.Exit(1)
	}
}

func LoadProvider(ctx context.Context) (genai.ChatProvider, error) {
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
