// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

//go:build ignore

// This tool downloads an HTML page from Ottawa's website and saves it unprocessed
// to generate a test case. It also generates the golden file containing the processed HTML
// to use as a reference for comparing extracted text in tests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/maruel/citygpt/internal"
	"github.com/maruel/genai"
)

func generateGoldens(ctx context.Context, c genai.ProviderGen) error {
	htmlFiles, err := filepath.Glob("testdata/*.html")
	if err != nil {
		return fmt.Errorf("failed to list HTML files: %w", err)
	}
	data := internal.Index{
		Version: 1,
		Created: time.Now(),
	}
	for _, htmlFile := range htmlFiles {
		fmt.Printf("Processing existing HTML file: %s\n", htmlFile)
		f, err := os.Open(htmlFile)
		if err != nil {
			return err
		}
		md := strings.TrimSuffix(htmlFile, filepath.Ext(htmlFile)) + ".md"
		title, summary, err := internal.ProcessHTML(ctx, c, f, md)
		_ = f.Close()
		if err != nil {
			return err
		}
		name := filepath.Base(md)
		item := internal.Item{Title: title, Summary: summary, Name: name, URL: "http://localhost:0/" + name}
		data.Items = append(data.Items, item)
	}
	b, err := json.MarshalIndent(data, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile("testdata/index.json", b, 0o644)
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unknown arguments")
	}
	c, err := internal.LoadProvider(ctx, "gemini", "", nil)
	if err != nil {
		return err
	}

	const targetURL = "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z/atv-orv-and-snowmobile-law-no-2019-421"
	fmt.Printf("Downloading HTML content from %s\n", targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	filename := filepath.Base(targetURL) + ".html"
	if err = os.WriteFile(filepath.Join("testdata", filename), content, 0o644); err != nil {
		return err
	}
	fmt.Printf("Successfully saved unprocessed HTML to %s\n", filename)
	return generateGoldens(ctx, c)
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "download_test_page: %v\n", err)
		os.Exit(1)
	}
}
