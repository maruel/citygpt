// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
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
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/maruel/citygpt/internal"
	"github.com/maruel/citygpt/internal/htmlparse"
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

type summaryWorkers struct {
	c             genai.ChatProvider
	outputDir     string
	previousIndex internal.Index
	urlLookup     map[string]int

	mu       sync.Mutex
	newIndex internal.Index
}

func urlToMDName(u string) (string, error) {
	parsedURL, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", u, err)
	}
	md := url.PathEscape(path.Base(strings.TrimSuffix(parsedURL.Path, "/")))
	if md == "" {
		md = "index"
	}
	return md + ".md", nil
}

func (s *summaryWorkers) worker(ctx context.Context, fullURL string) (bool, error) {
	// Always download a fresh copy of the HTML page in case it changed.
	resp, err := http.Get(fullURL)
	if err != nil {
		return false, fmt.Errorf("failed to fetch %s: %w", fullURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("received non-200 response for %s: %d", fullURL, resp.StatusCode)
	}
	md, title, err := htmlparse.ExtractTextFromHTML(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to extract text: %w", err)
	}

	mdName, err := urlToMDName(fullURL)
	if err != nil {
		return false, err
	}
	mdPath := filepath.Join(s.outputDir, mdName)
	now := time.Now().Round(time.Second)
	created := now
	if i, ok := s.urlLookup[fullURL]; ok {
		prev := s.previousIndex.Items[i]
		if prev.Name == mdName && prev.Title == title {
			// We saw this page in a previous run.
			if !prev.Created.IsZero() {
				created = prev.Created
			}
			if b, err := os.ReadFile(mdPath); err == nil && string(b) == md {
				// No need to re-create the summary, the content didn't change.
				s.mu.Lock()
				s.newIndex.Items = append(s.newIndex.Items, prev)
				s.mu.Unlock()
				return false, nil
			}
		}
	}

	// Content changed or is new, create a summary.
	item := internal.Item{URL: fullURL, Title: title, Name: mdName, Created: created, Modified: now}
	item.Summary, err = internal.Summarize(ctx, s.c, md)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	s.newIndex.Items = append(s.newIndex.Items, item)
	s.mu.Unlock()
	return true, nil
}

// downloadAndSaveTexts downloads content from links and saves the text using 8 workers in parallel
func downloadAndSaveTexts(ctx context.Context, c genai.ChatProvider, links []string, outputDir string) error {
	// Number of workers to process URLs in parallel
	const numWorkers = 8
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	// TODO: Recurse.
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
	indexPath := filepath.Join(outputDir, "index.json")
	b, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	now := time.Now().Round(time.Second)
	w := summaryWorkers{c: c, outputDir: outputDir, urlLookup: map[string]int{}, newIndex: internal.Index{Version: 1, Created: now, Modified: now}}
	if b != nil {
		if err = json.Unmarshal(b, &w.previousIndex); err != nil {
			return err
		}
		if len(w.previousIndex.Items) > 0 && !w.previousIndex.Created.IsZero() {
			w.newIndex.Created = w.previousIndex.Created
		}
		for i := range w.previousIndex.Items {
			w.urlLookup[w.previousIndex.Items[i].URL] = i
		}
	}
	type doneItem struct {
		url     string
		updated bool
	}
	jobs := make(chan string, 2*numWorkers)
	done := make(chan doneItem, 10)
	eg, ctx := errgroup.WithContext(ctx)
	for range numWorkers {
		eg.Go(func() error {
			for fullURL := range jobs {
				updated, err := w.worker(ctx, fullURL)
				if err != nil {
					return err
				}
				done <- doneItem{fullURL, updated}
			}
			return nil
		})
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		total := len(validLinks)
		processed := 0
		for i := range done {
			processed++
			suffix := ""
			if i.updated {
				suffix = " (updated)"
			}
			fmt.Printf("- (%d/%d): %s%s\n", processed, total, i.url, suffix)
		}
		wg.Done()
	}()
breakLoop:
	for _, url := range validLinks {
		select {
		case jobs <- url:
		case <-ctx.Done():
			break breakLoop
		}
	}
	close(jobs)
	err = eg.Wait()
	close(done)
	wg.Wait()
	sort.Slice(w.newIndex.Items, func(a, b int) bool {
		return w.newIndex.Items[a].URL < w.newIndex.Items[b].URL
	})
	// Always save, even in case of error.
	d, err2 := json.MarshalIndent(w.newIndex, "", " ")
	if err2 != nil {
		panic(err2)
	}
	if err2 = os.WriteFile(indexPath, d, 0o644); err == nil {
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
