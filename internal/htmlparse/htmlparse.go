// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package htmlparse provides common HTML processing utilities.
package htmlparse

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// ExtractTextFromHTML extracts and cleans text content from HTML
// It stops extracting text when encountering a div with id="ottux-footer"
func ExtractTextFromHTML(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}
	var textBuilder strings.Builder
	// Track if we've reached the footer section
	footerFound := false
	// Function to recursively extract text content
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		// Skip processing if we've already found the footer
		if footerFound {
			return
		}

		// Check if this is the footer div
		if n.Type == html.ElementNode && strings.ToLower(n.Data) == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "id" && attr.Val == "ottux-footer" {
					footerFound = true
					return
				}
			}
		}

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
	text = StripHTMLAndJSONBlocks(text)
	return text, nil
}

// StripHTMLAndJSONBlocks removes HTML tags and JSON-like content from the text
func StripHTMLAndJSONBlocks(text string) string {
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
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))) &&
			strings.Contains(trimmed, ":") && strings.Contains(trimmed, "\"") {
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
		if !isCodeNotJSON && jsonBlockStartLine == -1 && strings.Contains(trimmed, "{") {
			// Check for JSON-like patterns: has braces, quotes and colons
			if (strings.Contains(trimmed, ":") && strings.Contains(trimmed, "\"")) ||
				(strings.HasPrefix(trimmed, "{") &&
					(strings.Count(trimmed, "{") > 0 || strings.Count(trimmed, "\"") >= 2)) {
				jsonBlockStartLine = i
				jsonBlockLines = []string{trimmed}
				continue
			}
		}

		// If we're tracking a potential JSON block
		if jsonBlockStartLine >= 0 {
			jsonBlockLines = append(jsonBlockLines, trimmed)

			// Check if this line closes the JSON block
			if strings.Contains(trimmed, "}") {
				// Verify it looks like JSON by analyzing the combined content
				combined := strings.Join(jsonBlockLines, " ")

				// Count braces to make sure they match
				openCount := strings.Count(combined, "{")
				closeCount := strings.Count(combined, "}")

				// Check for JSON characteristics: matching braces, colons, quotes
				isLikelyJSON := openCount > 0 && closeCount > 0 &&
					openCount <= closeCount &&
					strings.Contains(combined, ":") &&
					strings.Contains(combined, "\"")

				if isLikelyJSON {
					// Skip the entire JSON block
					jsonBlockStartLine = -1
					jsonBlockLines = nil
					continue
				} else {
					// Not valid JSON but has closing braces
					// Check if first line might be normal text
					firstLine := lines[jsonBlockStartLine]
					if !strings.Contains(firstLine, "{") ||
						(!strings.Contains(firstLine, ":") && !strings.Contains(firstLine, "\"")) {
						filteredLines = append(filteredLines, firstLine)
					}
					jsonBlockStartLine = -1
					jsonBlockLines = nil
					continue
				}
			}

			// If we've accumulated too many lines without finding closing braces,
			// it's probably not JSON, so add the first line back and continue
			if len(jsonBlockLines) > 10 {
				firstLine := lines[jsonBlockStartLine]
				if !strings.HasPrefix(strings.TrimSpace(firstLine), "{") {
					filteredLines = append(filteredLines, firstLine)
				}
				jsonBlockStartLine = -1
				jsonBlockLines = nil
			}

			// Continue to the next line while we're tracking a JSON block
			continue
		}

		// If we get here, this is normal text that should be kept
		filteredLines = append(filteredLines, line)
	}

	// If we ended while still tracking a JSON block, add back any text that doesn't look like JSON
	if jsonBlockStartLine >= 0 && jsonBlockStartLine < len(lines) {
		firstLine := lines[jsonBlockStartLine]
		if !strings.HasPrefix(strings.TrimSpace(firstLine), "{") {
			filteredLines = append(filteredLines, firstLine)
		}
	}

	// Join the lines and remove any HTML tags that might remain
	text = strings.Join(filteredLines, "\n")

	// Remove consecutive empty lines
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text
}
