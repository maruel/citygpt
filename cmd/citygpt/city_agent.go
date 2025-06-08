// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/maruel/citygpt/internal"
	"github.com/maruel/genai"
	"github.com/maruel/genai/adapters"
)

type bylaws struct {
	Filename string `json:"filename"`
}

type cityAgent struct {
	c        genai.ProviderGen
	cityData fs.FS

	systemPrompt string
	toolPrompt   string
	index        internal.Index
}

func (c *cityAgent) init(cp genai.ProviderGen, cityData fs.FS) error {
	c.c = cp
	c.cityData = cityData
	if err := c.index.Load(c.cityData, "index.json"); err != nil {
		return err
	}
	content := make([]string, 0, len(c.index.Items))
	for _, item := range c.index.Items {
		content = append(content, fmt.Sprintf("- %s: %s", item.Name, item.Summary))
	}
	sort.Strings(content)
	c.systemPrompt = "You are a helpful assistant to answer the user's question grouned into documents.\n" +
		"The files provided by tool get_bylaws_text contain all the information to answer the user's questions. You can retrieve each of them by using the get_bylaws_text tool. Call get_bylaws_text tool at least once. The user wants to know how the bylaws applies to his topic or question."
	c.toolPrompt = "Returns the text of one of the bylaws for you to read. Here's a list of file names and their corresponding summary. Request as many of the following files as you need." +
		strings.Join(content, "\n")
	return nil
}

func (c *cityAgent) query(ctx context.Context, msgs genai.Messages) (genai.Messages, []string) {
	var files []string
	opts := genai.OptionsText{
		Seed:         1,
		SystemPrompt: c.systemPrompt,
		// Temperature: 0.1,
		Tools: []genai.ToolDef{
			{
				Name:        "get_bylaws_text",
				Description: c.toolPrompt,
				Callback: func(ctx context.Context, b *bylaws) (string, error) {
					slog.InfoContext(ctx, "citygpt", "msg", "requested file", "file", b.Filename)
					files = append(files, b.Filename)
					fileContent, err2 := fs.ReadFile(c.cityData, b.Filename)
					if err2 != nil {
						slog.ErrorContext(ctx, "citygpt", "msg", "Error reading selected file", "file", b.Filename, "err", err2)
						// Abort the conversation.
						return "", err2
					}
					return string(fileContent), nil
				},
			},
		},
	}
	slog.InfoContext(ctx, "citygpt", "query", len(msgs))
	if len(msgs) < 2 {
		opts.ToolCallRequest = genai.ToolCallRequired
	}
	newMsgs, usage, err := adapters.GenSyncWithToolCallLoop(ctx, c.c, msgs, &opts)
	if _, ok := err.(*genai.UnsupportedContinuableError); ok {
		err = nil
	}
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Error generating response", "err", err, "usage", usage)
		m := genai.Messages{genai.NewTextMessage(genai.Assistant, "Sorry, there was an error processing your request.")}
		return m, files
	}
	slog.InfoContext(ctx, "citygpt", "usage", usage)
	return newMsgs, files
}
