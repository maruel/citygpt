// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Package main provides a tool to extract and download text content from Ottawa's website.
package main

import (
	"bufio"
	"bytes"
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
	// TargetURL is the URL to fetch links from
	TargetURL = "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z"

	// BaseURL is the base URL for resolving relative links
	BaseURL = "https://ottawa.ca/en/living-ottawa/"

	// OutputDir is where downloaded text files will be stored
	OutputDir = "pages_text"

	// LinksFile is the file where extracted links will be stored
	LinksFile = "links.txt"
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
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	for _, link := range links {
		if _, err := file.WriteString(link + "\n"); err != nil {
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
	return strings.HasPrefix(link, BaseURL)
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
	return strings.TrimSpace(textBuilder.String()), nil
}

// downloadAndSaveTexts downloads content from links and saves the text
func downloadAndSaveTexts(linksFile, outputDir string) error {
	// Ensure output directory exists
	err := os.MkdirAll(outputDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Read links from file
	content, err := os.ReadFile(linksFile)
	if err != nil {
		return fmt.Errorf("failed to read links file: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		link := scanner.Text()
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}

		// Construct full URL from relative link
		fullURL := link
		if strings.HasPrefix(link, "/") {
			fullURL = "https://ottawa.ca" + link
		}

		// Skip links not under BaseURL or with bad extensions
		if !isValidContentURL(fullURL) {
			fmt.Printf("Skipping link: %s\n", fullURL)
			continue
		}

		fmt.Printf("Fetching: %s\n", fullURL)

		// Download the content
		resp, err := http.Get(fullURL)
		if err != nil {
			fmt.Printf("Failed to fetch %s: %v\n", fullURL, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Received non-200 response for %s: %d\n", fullURL, resp.StatusCode)
			resp.Body.Close()
			continue
		}

		// Extract text content
		textContent, err := extractTextFromHTML(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Printf("Failed to extract text from %s: %v\n", fullURL, err)
			continue
		}

		// Generate a safe filename
		parsedURL, err := url.Parse(fullURL)
		if err != nil {
			fmt.Printf("Failed to parse URL %s: %v\n", fullURL, err)
			continue
		}

		filename := strings.TrimPrefix(parsedURL.Path, "/")
		filename = strings.ReplaceAll(filename, "/", "_")
		if filename == "" {
			filename = "index"
		}

		filename = url.PathEscape(filename) + ".txt"
		filePath := filepath.Join(outputDir, filename)

		// Save to text file
		err = os.WriteFile(filePath, []byte(textContent), 0644)
		if err != nil {
			fmt.Printf("Failed to write file %s: %v\n", filePath, err)
			continue
		}
	}

	return nil
}

func main() {
	// Define command line flags
	extractOnly := flag.Bool("extract-only", false, "Only extract links without downloading content")
	downloadOnly := flag.Bool("download-only", false, "Only download content using existing links file")
	outputDir := flag.String("output-dir", OutputDir, "Directory to save downloaded text files")
	linksFile := flag.String("links-file", LinksFile, "File to save extracted links")
	flag.Parse()

	// If no flags specified, run both extraction and download
	runExtract := !*downloadOnly
	runDownload := !*extractOnly

	if runExtract {
		fmt.Printf("Extracting links from %s\n", TargetURL)
		links, err := extractLinks(TargetURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting links: %v\n", err)
			os.Exit(1)
		}

		if err := writeLinksToFile(links, *linksFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing links to file: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Extracted %d links to %s\n", len(links), *linksFile)
	}

	if runDownload {
		fmt.Printf("Downloading and processing content from links in %s\n", *linksFile)
		if err := downloadAndSaveTexts(*linksFile, *outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading texts: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Process completed successfully")
}
