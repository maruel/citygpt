// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// ottawa-data extracts and downloads text content from Ottawa's website.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/maruel/citygpt/internal"
	"github.com/maruel/genai"
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

// downloadAndSaveTexts downloads content from links and saves the text using 8 workers in parallel
func downloadAndSaveTexts(ctx context.Context, c genai.ChatProvider, links []string, outputDir string) error {
	// Number of workers to process URLs in parallel
	const numWorkers = 8
	err := os.MkdirAll(outputDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	var validLinks []string
	for _, link := range links {
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
	eg, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var processed atomic.Int32
	data := internal.Index{Version: 1, Created: time.Now()}
	for range numWorkers {
		eg.Go(func() error {
			for fullURL := range jobs {
				s, err := internal.ProcessURL(ctx, c, fullURL, outputDir)
				if err != nil {
					return err
				}
				count := processed.Add(1)
				mu.Lock()
				data.Items = append(data.Items, s)
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
	err = eg.Wait()
	d, err2 := json.Marshal(data)
	if err2 != nil {
		panic(err2)
	}
	if err2 := os.WriteFile(filepath.Join(outputDir, "index.json"), d, 0o644); err == nil {
		err = err2
	}
	return err
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	outputDir := flag.String("output-dir", "pages_text", "Directory to save downloaded markdown files")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unknown arguments")
	}
	c, err := internal.LoadProvider(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Extracting links from %s\n", targetURL)
	links, err := extractLinks(targetURL)
	if err != nil {
		return fmt.Errorf("error extracting links: %w", err)
	}
	if err := downloadAndSaveTexts(ctx, c, links, *outputDir); err != nil {
		return fmt.Errorf("error downloading texts: %w", err)
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
