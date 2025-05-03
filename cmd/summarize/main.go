// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// summarize is a command-line tool that uses Cerebras API to generate summaries of text files.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/citygpt/internal"
	"github.com/maruel/genai"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "summarize: %v\n", err)
		os.Exit(1)
	}
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

	timeoutFlag := flag.Duration("timeout", 2*time.Minute, "Timeout for the API request")
	flag.Parse()

	if flag.NArg() != 1 {
		return fmt.Errorf("expected a single filename argument. Usage: summarize [flags] <filename>")
	}

	fileContent, err := readFile(flag.Arg(0))
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	ctxWithTimeout, cancel := context.WithTimeout(ctx, *timeoutFlag)
	defer cancel()
	c, err := internal.LoadProvider(ctx)
	if err != nil {
		return err
	}
	messages := genai.Messages{
		genai.NewTextMessage(genai.User, "You are a helpful assistant that summarizes text content accurately and concisely."),
		genai.NewTextMessage(genai.User, "Please summarize the subject of following text:"),
		genai.NewTextMessage(genai.User, fileContent),
	}
	opts := genai.ChatOptions{Seed: 1, Temperature: 0.3}
	slog.Info("Generating summary...")
	resp, err := c.Chat(ctxWithTimeout, messages, &opts)
	if err != nil {
		return fmt.Errorf("failed to get summary: %w", err)
	}
	for _, content := range resp.Contents {
		if content.Text != "" {
			fmt.Println(content.Text)
		} else if content.Document != nil || content.URL != "" {
			fmt.Println("Received document or URL content (not displaying)")
		}
	}
	return nil
}

// readFile reads the content of a file.
func readFile(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
