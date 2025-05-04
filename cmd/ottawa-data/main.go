// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// ottawa-data extracts and downloads text content from Ottawa's website.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

const (
	// targetURL is the URL to fetch links from
	targetURL = "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z"

	// baseURL is the base URL for resolving relative links
	baseURL = targetURL + "/"
)

// Extensions to ignore when processing URLs
var ignoreExtensions = []string{
	".ico", ".jpg", ".jpeg", ".png", ".gif", ".svg",
	".bmp", ".webp", ".pdf", ".css", ".js",
}

// extractLinks fetches a webpage and extracts all href attributes
func extractLinks(url string) ([]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
	var links []string
	// Function to recursively extract links from HTML
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					links = append(links, attr.Val)
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)
	return links, nil
}

// writeLinksToFile writes the extracted links to a file
func writeLinksToFile(links []string, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()
	for _, link := range links {
		if _, err := f.WriteString(link + "\n"); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
	}
	return nil
}

// isValidContentURL checks if a URL should be processed
func isValidContentURL(link string) bool {
	// Check if the URL has any of the ignored extensions
	linkLower := strings.ToLower(link)
	for _, ext := range ignoreExtensions {
		if strings.HasSuffix(linkLower, ext) {
			return false
		}
	}
	// Check if the URL is within the base URL
	return strings.HasPrefix(link, baseURL)
}

// extractTextFromHTML extracts and cleans text content from HTML
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

// downloadAndSaveTexts downloads content from links and saves the text
func downloadAndSaveTexts(linksFile, outputDir string) error {
	// Ensure output directory exists
	err := os.MkdirAll(outputDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	// Read links from file
	content, err := os.ReadFile(linksFile)
	if err != nil {
		return fmt.Errorf("failed to read links file: %w", err)
	}

	// First, count the total number of valid links
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var validLinks []string
	for scanner.Scan() {
		link := strings.TrimSpace(scanner.Text())
		if link == "" {
			continue
		}
		// Construct full URL from relative link
		fullURL := link
		if strings.HasPrefix(link, "/") {
			fullURL = "https://ottawa.ca" + link
		}
		if isValidContentURL(fullURL) {
			validLinks = append(validLinks, fullURL)
		}
	}
	total := len(validLinks)

	// Process each valid link
	for index, fullURL := range validLinks {
		index := index + 1 // Make it 1-based for user display
		fmt.Printf("Fetching (%d/%d): %s\n", index, total, fullURL)

		// Download the content
		resp, err := http.Get(fullURL)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", fullURL, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("received non-200 response for %s: %d", fullURL, resp.StatusCode)
		}

		// Extract text content
		textContent, err := extractTextFromHTML(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to extract text from %s: %w", fullURL, err)
		}

		// Generate a safe filename
		parsedURL, err := url.Parse(fullURL)
		if err != nil {
			return fmt.Errorf("failed to parse URL %s: %w", fullURL, err)
		}

		filename := strings.TrimPrefix(parsedURL.Path, "/")
		filename = strings.ReplaceAll(filename, "/", "_")
		if filename == "" {
			filename = "index"
		}
		filename = url.PathEscape(filename) + ".txt"
		filePath := filepath.Join(outputDir, filename)
		err = os.WriteFile(filePath, []byte(textContent), 0o644)
		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
	}
	return nil
}

func mainImpl() error {
	extractOnly := flag.Bool("extract-only", false, "Only extract links without downloading content")
	downloadOnly := flag.Bool("download-only", false, "Only download content using existing links file")
	outputDir := flag.String("output-dir", "pages_text", "Directory to save downloaded text files")
	linksFile := flag.String("links-file", "links.txt", "File to save extracted links")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unknown arguments")
	}

	// If no flags specified, run both extraction and download
	runExtract := !*downloadOnly
	runDownload := !*extractOnly
	if runExtract {
		fmt.Printf("Extracting links from %s\n", targetURL)
		links, err := extractLinks(targetURL)
		if err != nil {
			return fmt.Errorf("error extracting links: %w", err)
		}

		if err := writeLinksToFile(links, *linksFile); err != nil {
			return fmt.Errorf("error writing links to file: %w", err)
		}
		fmt.Printf("Extracted %d links to %s\n", len(links), *linksFile)
	}

	if runDownload {
		fmt.Printf("Downloading and processing content from links in %s\n", *linksFile)
		if err := downloadAndSaveTexts(*linksFile, *outputDir); err != nil {
			return fmt.Errorf("error downloading texts: %w", err)
		}
	}
	fmt.Println("Process completed successfully")
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "ottawa-data: %s\n", err)
		os.Exit(1)
	}
}

// stripHTMLAndJSONBlocks removes HTML tags and JSON-like content from the text
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
		text = strings.Replace(text, "\n\n\n", "\n\n", -1)
	}

	return text
}
