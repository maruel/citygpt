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
// If a div with id="block-mainpagecontent" exists, it only extracts content from that div
// Otherwise, it extracts all content from the document
func ExtractTextFromHTML(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}
	var textBuilder *strings.Builder = &strings.Builder{}
	// Function to recursively extract text content
	var extractText func(*html.Node)

	// Function to convert HTML table to Markdown
	processTable := func(tableNode *html.Node) string {
		var mdTable strings.Builder

		// Helper to process table rows
		processRow := func(tr *html.Node, cellTag string) []string {
			var cells []string
			for td := tr.FirstChild; td != nil; td = td.NextSibling {
				if td.Type == html.ElementNode && strings.ToLower(td.Data) == cellTag {
					var cellContent strings.Builder
					for c := td.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.TextNode {
							cellContent.WriteString(strings.TrimSpace(c.Data))
						} else if c.Type == html.ElementNode {
							// Process nested elements in cells (like <strong>, <em>, etc.)
							for tc := c.FirstChild; tc != nil; tc = tc.NextSibling {
								if tc.Type == html.TextNode {
									cellContent.WriteString(strings.TrimSpace(tc.Data))
								}
							}
						}
					}
					cells = append(cells, cellContent.String())
				}
			}
			return cells
		}

		// Find table headers and rows
		var headers []string
		var rows [][]string

		// Process thead if present
		for thead := tableNode.FirstChild; thead != nil; thead = thead.NextSibling {
			if thead.Type == html.ElementNode && strings.ToLower(thead.Data) == "thead" {
				for tr := thead.FirstChild; tr != nil; tr = tr.NextSibling {
					if tr.Type == html.ElementNode && strings.ToLower(tr.Data) == "tr" {
						headers = processRow(tr, "th")
						if len(headers) == 0 {
							// Some tables use td in thead instead of th
							headers = processRow(tr, "td")
						}
						break
					}
				}
			}
		}

		// Process tbody
		for tbody := tableNode.FirstChild; tbody != nil; tbody = tbody.NextSibling {
			if tbody.Type == html.ElementNode {
				if strings.ToLower(tbody.Data) == "tbody" {
					// Check the first row for th elements
					var firstRow *html.Node
					for tr := tbody.FirstChild; tr != nil; tr = tr.NextSibling {
						if tr.Type == html.ElementNode && strings.ToLower(tr.Data) == "tr" {
							firstRow = tr
							break
						}
					}

					// If we have a first row, check if it contains th elements
					if firstRow != nil && len(headers) == 0 {
						headers = processRow(firstRow, "th")
						// If first row has th elements, it's a header row
						hasHeaders := len(headers) > 0

						// Process all rows
						for tr := tbody.FirstChild; tr != nil; tr = tr.NextSibling {
							if tr.Type == html.ElementNode && strings.ToLower(tr.Data) == "tr" {
								// Skip the first row if it was a header
								if hasHeaders && tr == firstRow {
									continue
								}
								cells := processRow(tr, "td")
								if len(cells) > 0 {
									rows = append(rows, cells)
								}
							}
						}
					} else {
						// No headers or no first row, just process all rows as data
						for tr := tbody.FirstChild; tr != nil; tr = tr.NextSibling {
							if tr.Type == html.ElementNode && strings.ToLower(tr.Data) == "tr" {
								cells := processRow(tr, "td")
								if len(cells) > 0 {
									rows = append(rows, cells)
								}
							}
						}
					}
				} else if strings.ToLower(tbody.Data) == "tr" {
					// Handle tables without tbody
					// If no headers found yet and this is the first row, check for th elements
					if len(headers) == 0 && len(rows) == 0 {
						headers = processRow(tbody, "th")
						if len(headers) == 0 {
							// No th elements, so it's a regular row
							cells := processRow(tbody, "td")
							if len(cells) > 0 {
								rows = append(rows, cells)
							}
						}
					} else {
						cells := processRow(tbody, "td")
						if len(cells) > 0 {
							rows = append(rows, cells)
						}
					}
				}
			}
		}

		// If no rows or cells found, return empty string
		if len(rows) == 0 && len(headers) == 0 {
			return ""
		}

		// Calculate column count based on the row with most cells
		columnCount := len(headers)
		for _, row := range rows {
			if len(row) > columnCount {
				columnCount = len(row)
			}
		}

		// If no headers, create blank ones for the Markdown format
		if len(headers) == 0 {
			headers = make([]string, columnCount)
			for i := range headers {
				headers[i] = ""
			}
		} else if len(headers) < columnCount {
			// Extend headers if needed
			for i := len(headers); i < columnCount; i++ {
				headers = append(headers, "")
			}
		}

		// Generate the Markdown table

		// Headers row
		mdTable.WriteString("|")
		for _, h := range headers {
			mdTable.WriteString(" ")
			mdTable.WriteString(h)
			mdTable.WriteString(" |")
		}
		mdTable.WriteString("\n")

		// Separator row
		mdTable.WriteString("|")
		for range headers {
			mdTable.WriteString(" ------- |")
		}
		mdTable.WriteString("\n")

		// Data rows
		for _, row := range rows {
			mdTable.WriteString("|")
			for i := range make([]struct{}, columnCount) {
				mdTable.WriteString(" ")
				if i < len(row) {
					mdTable.WriteString(row[i])
				}
				mdTable.WriteString(" |")
			}
			mdTable.WriteString("\n")
		}

		return mdTable.String()
	}

	// Helper function to process ordered and unordered lists
	processList := func(listNode *html.Node, isOrdered bool) string {
		var listBuilder strings.Builder
		var processListItems func(*html.Node, string, int)

		// Process list items recursively with indentation
		processListItems = func(node *html.Node, prefix string, level int) {
			for li := node.FirstChild; li != nil; li = li.NextSibling {
				if li.Type == html.ElementNode && strings.ToLower(li.Data) == "li" {
					indent := strings.Repeat("  ", level-1)
					listBuilder.WriteString(indent)
					listBuilder.WriteString(prefix)

					// Process the content of the list item
					for c := li.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.TextNode {
							text := strings.TrimSpace(c.Data)
							if text != "" {
								listBuilder.WriteString(text)
							}
						} else if c.Type == html.ElementNode && (strings.ToLower(c.Data) != "ol" && strings.ToLower(c.Data) != "ul") {
							// For non-list elements inside list items
							tempBuilder := &strings.Builder{}
							// Save the reference to avoid modifying the original textBuilder
							savedBuilder := textBuilder
							textBuilder = tempBuilder
							extractText(c)
							text := strings.TrimSpace(tempBuilder.String())
							if text != "" {
								listBuilder.WriteString(text)
							}
							// Restore the original textBuilder
							textBuilder = savedBuilder
						}
					}

					listBuilder.WriteString("\n")

					// Process nested lists
					for c := li.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode {
							if strings.ToLower(c.Data) == "ul" {
								// Nested unordered list
								for subLi := c.FirstChild; subLi != nil; subLi = subLi.NextSibling {
									if subLi.Type == html.ElementNode && strings.ToLower(subLi.Data) == "li" {
										processListItems(c, "- ", level+1)
										break
									}
								}
							} else if strings.ToLower(c.Data) == "ol" {
								// Nested ordered list
								for subLi := c.FirstChild; subLi != nil; subLi = subLi.NextSibling {
									if subLi.Type == html.ElementNode && strings.ToLower(subLi.Data) == "li" {
										processListItems(c, "1. ", level+1)
										break
									}
								}
							}
						}
					}
				}
			}
		}

		listPrefix := "- "
		if isOrdered {
			listPrefix = "1. "
		}

		processListItems(listNode, listPrefix, 1)

		return listBuilder.String()
	}

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

			// Handle tables by converting them to Markdown
			if tagName == "table" {
				markdownTable := processTable(n)
				if markdownTable != "" {
					textBuilder.WriteString(markdownTable)
					textBuilder.WriteString("\n")
				}
				return // Skip further processing of the table's children
			}

			// Handle ordered and unordered lists
			if tagName == "ul" || tagName == "ol" {
				isOrdered := tagName == "ol"
				markdown := processList(n, isOrdered)
				if markdown != "" {
					textBuilder.WriteString(markdown)
					textBuilder.WriteString("\n")
				}
				return // Skip further processing of the list's children
			}

			// Handle headers h1-h6
			if len(tagName) == 2 && tagName[0] == 'h' && tagName[1] >= '1' && tagName[1] <= '6' {
				level := int(tagName[1] - '0')
				headerPrefix := strings.Repeat("#", level) + " "

				// Get the text content of the header
				var headerText string
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						headerText += strings.TrimSpace(c.Data)
					}
				}

				if headerText != "" {
					textBuilder.WriteString(headerPrefix)
					textBuilder.WriteString(headerText)
					textBuilder.WriteString("\n\n")
				}
				return // Skip further processing of header's children
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

	// Find the div with id="block-mainpagecontent"
	var findAndExtractMainContent func(*html.Node) bool
	findAndExtractMainContent = func(n *html.Node) bool {
		if n.Type == html.ElementNode && strings.ToLower(n.Data) == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "id" && attr.Val == "block-mainpagecontent" {
					// Found the main content div, extract text from it
					extractText(n)
					return true
				}
			}
		}

		// Recursively search child nodes
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if findAndExtractMainContent(c) {
				return true
			}
		}

		return false
	}

	// Try to find the block-mainpagecontent div first
	if !findAndExtractMainContent(doc) {
		// If block-mainpagecontent not found, extract all content
		textBuilder.Reset() // Clear any partial content
		extractText(doc)
	}

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
