// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
)

func mainImpl() error {
	c, err := cerebras.New("", "llama-3.1-8b")
	if err != nil {
		return err
	}
	msgs := genai.Messages{
		genai.NewTextMessage(genai.User, "Is Ottawa great?"),
	}
	opts := genai.ChatOptions{
		Seed:        1,
		Temperature: 0.01,
		MaxTokens:   50,
	}
	ctx := context.Background()
	resp, err := c.Chat(ctx, msgs, &opts)
	if err != nil {
		return err
	}
	println(resp.Contents[0].Text)
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "citygpt: %s\n", err)
		os.Exit(1)
	}
}
