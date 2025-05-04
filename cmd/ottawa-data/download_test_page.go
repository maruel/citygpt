// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

//go:build ignore

// This tool downloads an HTML page from Ottawa's website and saves it unprocessed
// to generate a test case. It also generates the golden file containing the processed HTML
// to use as a reference for comparing extracted text in tests.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/maruel/citygpt/internal/htmlparse"
)

// processHTMLFile processes an HTML file and generates its golden file
func processHTMLFile(htmlFilePath string) error {
	// Open the HTML file
	htmlFile, err := os.Open(htmlFilePath)
	if err != nil {
		return fmt.Errorf("failed to open HTML file: %w", err)
	}
	defer htmlFile.Close()

	// Extract text and title from the HTML
	textContent, _, err := htmlparse.ExtractTextFromHTML(htmlFile)
	if err != nil {
		return fmt.Errorf("failed to extract text: %w", err)
	}
	md := strings.TrimSuffix(htmlFilePath, filepath.Ext(htmlFilePath)) + ".md"
	if err = os.WriteFile(md, []byte(textContent), 0o644); err != nil {
		return fmt.Errorf("failed to write golden file: %w", err)
	}

	fmt.Printf("Successfully generated golden file: %s\n", md)
	return nil
}

func mainImpl() error {
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

	// Save the raw HTML file
	filename := filepath.Base(targetURL) + ".html"
	if err = os.WriteFile(filepath.Join("testdata", filename), content, 0o644); err != nil {
		return err
	}
	fmt.Printf("Successfully saved unprocessed HTML to %s\n", filename)

	htmlFiles, err := filepath.Glob("testdata/*.html")
	if err != nil {
		return fmt.Errorf("failed to list HTML files: %w", err)
	}
	for _, htmlFile := range htmlFiles {
		fmt.Printf("Processing existing HTML file: %s\n", htmlFile)
		if err := processHTMLFile(htmlFile); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "download_test_page: %v\n", err)
		os.Exit(1)
	}
}
