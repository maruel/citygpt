// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

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

	"golang.org/x/net/html"
)

// extractTextFromHTML extracts and cleans text content from HTML
// This is a duplicate of the function in main.go to ensure consistent processing
func extractTextFromHTML(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}
	var textBuilder strings.Builder
	// Function to recursively extract text content
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		// Skip code blocks, pre blocks, and script elements
		if n.Type == html.ElementNode {
			tagName := strings.ToLower(n.Data)
			// Skip elements that typically contain code or styling
			if tagName == "pre" || tagName == "code" || tagName == "script" ||
				tagName == "style" || tagName == "iframe" || tagName == "svg" ||
				tagName == "canvas" || tagName == "noscript" {
				return
			}
		}

		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				textBuilder.WriteString(text)
				textBuilder.WriteString("\n")
			}
		}

		// Recursively process child nodes
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractText(c)
		}
	}

	extractText(doc)

	// Post-process to remove JSON blocks and any remaining HTML tags
	text := strings.TrimSpace(textBuilder.String())
	text = stripHTMLAndJSONBlocks(text)
	return text, nil
}

// stripHTMLAndJSONBlocks removes HTML tags and JSON-like content from the text
// This is a duplicate of the function in main.go to ensure consistent processing
func stripHTMLAndJSONBlocks(text string) string {
	// First, do a simple HTML tag removal for standalone tags
	lines := strings.Split(text, "\n")
	var filteredLines []string

	// Track if we're inside a JSON-like block
	var jsonBlockStartLine int = -1
	var jsonBlockLines []string

	// Whitelist of programming language constructs that use braces
	// but should not be treated as JSON
	codePrefixes := []string{
		"func ", "function ", "def ", "class ", "if ", "for ", "while ",
		"switch ", "public ", "private ", "protected ", "void ", "int ",
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			filteredLines = append(filteredLines, "")
			continue
		}

		// Check for standalone HTML tags to remove
		if (strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">")) ||
			(strings.Contains(trimmed, "</") && strings.Contains(trimmed, ">")) ||
			(strings.HasPrefix(trimmed, "<!") && strings.HasSuffix(trimmed, ">")) {
			continue
		}

		// Skip lines that are likely to be JSON objects
		if ((strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))&&
			strings.Contains(trimmed, ":") && strings.Contains(trimmed, "\"")) {
			continue
		}

		// Check if this is code rather than JSON (contains code-like prefixes)
		isCodeNotJSON := false
		for _, prefix := range codePrefixes {
			if strings.Contains(trimmed, prefix) {
				isCodeNotJSON = true
				break
			}
		}

		// Start detecting multi-line JSON blocks
		if !isCodeNotJSON && jsonBlockStartLine == -1 && strings.HasPrefix(trimmed, "{") &&
			!strings.HasSuffix(trimmed, "}") && strings.Contains(trimmed, ":") {
			jsonBlockStartLine = i
			jsonBlockLines = append(jsonBlockLines, trimmed)
			continue
		}

		// Continue collecting JSON block lines
		if jsonBlockStartLine != -1 {
			jsonBlockLines = append(jsonBlockLines, trimmed)

			// Check if this is the end of a JSON block
			if strings.HasSuffix(trimmed, "}") {
				// Check the whole block - does it look like JSON?
				completeBlock := strings.Join(jsonBlockLines, " ")
				if strings.Count(completeBlock, ":") > 2 && strings.Count(completeBlock, "\"") > 4 {
					// This looks like a JSON block, skip it
					jsonBlockStartLine = -1
					jsonBlockLines = nil
					continue
				} else {
					// Not a JSON block after all, add all the lines
					filteredLines = append(filteredLines, lines[jsonBlockStartLine:i+1]...)
					jsonBlockStartLine = -1
					jsonBlockLines = nil
					continue
				}
			}
			continue
		}

		filteredLines = append(filteredLines, line)
	}

	return strings.Join(filteredLines, "\n")
}

// processHTMLFile processes an HTML file and generates its golden file
func processHTMLFile(htmlFilePath string) error {
	// Open the HTML file
	htmlFile, err := os.Open(htmlFilePath)
	if err != nil {
		return fmt.Errorf("failed to open HTML file: %w", err)
	}
	defer htmlFile.Close()

	// Extract text from the HTML
	textContent, err := extractTextFromHTML(htmlFile)
	if err != nil {
		return fmt.Errorf("failed to extract text: %w", err)
	}

	// Write the extracted text to the golden file
	goldenFilePath := htmlFilePath + ".golden"
	if err = os.WriteFile(goldenFilePath, []byte(textContent), 0644); err != nil {
		return fmt.Errorf("failed to write golden file: %w", err)
	}

	fmt.Printf("Successfully generated golden file: %s\n", goldenFilePath)
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
	htmlFilePath := filename // Since we're already in the testdata directory
	if err = os.WriteFile(htmlFilePath, content, 0o644); err != nil {
		return err
	}
	fmt.Printf("Successfully saved unprocessed HTML to %s\n", filename)

	// Process the HTML file and generate the golden file
	if err := processHTMLFile(htmlFilePath); err != nil {
		return err
	}

	// Also process any other HTML files in the current directory
	htmlFiles, err := filepath.Glob("*.html")
	if err != nil {
		return fmt.Errorf("failed to list HTML files: %w", err)
	}
	for _, htmlFile := range htmlFiles {
		// Skip the file we just downloaded as we've already processed it
		if htmlFile == htmlFilePath {
			continue
		}
		
		fmt.Printf("Processing existing HTML file: %s\n", htmlFile)
		if err := processHTMLFile(htmlFile); err != nil {
			return err
		}
	}

	fmt.Printf("\nReminder: Don't forget to add the new files to git:\n")
	fmt.Printf("git add %s %s.golden\n", htmlFilePath, htmlFilePath)
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "download_test_page: %v\n", err)
		os.Exit(1)
	}
}
