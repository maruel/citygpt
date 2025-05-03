// Copyright 2024 Marc-Antoine Ruel. All rights reserved.
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
	"time"

	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
)

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	modelFlag := flag.String("model", "llama3.1-8b", "Model to use for chat completions")
	timeoutFlag := flag.Duration("timeout", 2*time.Minute, "Timeout for the API request")
	flag.Parse()

	if flag.NArg() != 1 {
		return fmt.Errorf("expected a single filename argument. Usage: summarize [flags] <filename>")
	}
	
	filename := flag.Arg(0)

	// Read the file content
	fileContent, err := readFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Create a context with timeout
	ctxWithTimeout, cancel := context.WithTimeout(ctx, *timeoutFlag)
	defer cancel()

	// Initialize the Cerebras client
	c, err := cerebras.New("", *modelFlag)
	if err != nil {
		return fmt.Errorf("failed to initialize Cerebras client: %w", err)
	}

	// Create a message to send to the LLM
	messages := genai.Messages{
		genai.NewTextMessage("system", "You are a helpful assistant that summarizes text content accurately and concisely."),
		genai.NewTextMessage("user", fmt.Sprintf("Please summarize the following text:\n\n%s", fileContent)),
	}

	// Set up chat options
	opts := genai.ChatOptions{Seed: 1, Temperature: 0.3}

	// Call the Chat function
	slog.Info("Generating summary...")
	resp, err := c.Chat(ctxWithTimeout, messages, &opts)
	if err != nil {
		return fmt.Errorf("failed to get summary: %w", err)
	}

	// Print the summary
	fmt.Println("Summary:")
	for _, content := range resp.Message.Contents {
		// Extract text from the content
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
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
