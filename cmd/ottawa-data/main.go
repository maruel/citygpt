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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/maruel/citygpt/internal/htmlparse"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
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

// NOTE: HTML parsing functionality has been moved to internal/htmlparse package

// downloadAndSaveTexts downloads content from links and saves the text using 8 workers in parallel
func downloadAndSaveTexts(linksFile, outputDir string) error {
	// Number of workers to process URLs in parallel
	const numWorkers = 8
	err := os.MkdirAll(outputDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	content, err := os.ReadFile(linksFile)
	if err != nil {
		return fmt.Errorf("failed to read links file: %w", err)
	}
	var validLinks []string
	for scanner := bufio.NewScanner(bytes.NewReader(content)); scanner.Scan(); {
		link := strings.TrimSpace(scanner.Text())
		if link == "" {
			continue
		}
		// Construct full URL from relative link
		if strings.HasPrefix(link, "/") {
			link = "https://ottawa.ca" + link
		}
		if isValidContentURL(link) {
			validLinks = append(validLinks, link)
		}
	}
	total := len(validLinks)
	jobs := make(chan string, total)
	var eg errgroup.Group
	var mu sync.Mutex
	var processed atomic.Int32
	for range numWorkers {
		eg.Go(func() error {
			for fullURL := range jobs {
				if err := processURL(fullURL, outputDir); err != nil {
					return err
				}
				count := processed.Add(1)
				mu.Lock()
				fmt.Printf("Fetched (%d/%d): %s\n", count, total, fullURL)
				mu.Unlock()
			}
			return nil
		})
	}
	for _, url := range validLinks {
		jobs <- url
	}
	close(jobs)
	return eg.Wait()
}

// processURL downloads text from a single URL and saves it
func processURL(fullURL, outputDir string) error {
	resp, err := http.Get(fullURL)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", fullURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fmt.Errorf("received non-200 response for %s: %d", fullURL, resp.StatusCode)
	}
	textContent, err := htmlparse.ExtractTextFromHTML(resp.Body)
	_ = resp.Body.Close()
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
	if err = os.WriteFile(filePath, []byte(textContent), 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
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
