// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

// ottawa-data extracts and downloads text content from Ottawa's website.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

func trimURLFragment(u string) (string, error) {
	parsedURL, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	parsedURL.Fragment = ""
	return parsedURL.String(), nil
}

// extractLinksFromURL fetches a webpage and extracts all href attributes
func extractLinksFromURL(u string) ([]string, error) {
	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	// TODO: Check content-type?
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}
	return extractLinks(u, resp.Body)
}

func extractLinks(u string, r io.Reader) ([]string, error) {
	parsedURL, err := url.Parse(u)
	if err != nil {
		return nil, nil
	}
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
	// baseURL is the base URL for resolving relative links
	baseURL := u + "/"
	host := parsedURL.Scheme + "://" + parsedURL.Host
	var links []string
	// Function to recursively extract links from HTML
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					link := attr.Val
					// Check if it is a link we care about.
					// Construct full URL from relative link
					if strings.HasPrefix(link, "/") {
						link = host + link
					}
					if isValidContentURL(link, baseURL) {
						var err2 error
						if link, err2 = trimURLFragment(link); err2 == nil {
							links = append(links, link)
						}
					}
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
func isValidContentURL(link, baseURL string) bool {
	if !strings.HasPrefix(link, baseURL) {
		return false
	}
	switch strings.ToLower(path.Ext(link)) {
	case ".ico", ".jpg", ".jpeg", ".png", ".gif", ".svg", ".bmp", ".webp", ".pdf", ".css", ".js":
		// Common extensions to ignore when processing URLs.
		// TODO: Process pdf.
		return false
	default:
		return true
	}
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

func (s *summaryWorkers) worker(ctx context.Context, fullURL string) (bool, []string, error) {
	// Always download a fresh copy of the HTML page in case it changed.
	resp, err := http.Get(fullURL)
	if err != nil {
		return false, nil, fmt.Errorf("failed to fetch %s: %w", fullURL, err)
	}
	b, err := io.ReadAll(resp.Body)
	if err2 := resp.Body.Close(); err == nil {
		err = err2
	}
	if err != nil {
		return false, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 404 {
			// The web site has broken links.
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("received non-200 response for %s: %d", fullURL, resp.StatusCode)
	}

	links, err := extractLinks(fullURL, bytes.NewReader(b))
	if err != nil {
		return false, links, err
	}
	md, title, err := htmlparse.ExtractTextFromHTML(bytes.NewReader(b))
	if err != nil {
		return false, links, fmt.Errorf("failed to extract text: %w", err)
	}

	mdName, err := urlToMDName(fullURL)
	if err != nil {
		return false, links, err
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
			if b, err2 := os.ReadFile(mdPath); err2 == nil && string(b) == md {
				// No need to re-create the summary, the content didn't change.
				s.mu.Lock()
				s.newIndex.Items = append(s.newIndex.Items, prev)
				s.mu.Unlock()
				return false, links, nil
			}
		}
	}

	// Content changed or is new, create a summary.
	if err = os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return false, links, err
	}
	item := internal.Item{URL: fullURL, Title: title, Name: mdName, Created: created, Modified: now}
	item.Summary, err = internal.Summarize(ctx, s.c, md)
	if err != nil {
		return false, links, err
	}
	s.mu.Lock()
	s.newIndex.Items = append(s.newIndex.Items, item)
	s.mu.Unlock()
	return true, links, nil
}

// downloadAndSaveTexts downloads content from links and saves the text using 8 workers in parallel
func downloadAndSaveTexts(ctx context.Context, c genai.ChatProvider, baseURL string, outputDir string) error {
	// Number of workers to process URLs in parallel
	const numWorkers = 8
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
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

	links, err := extractLinksFromURL(baseURL)
	if err != nil {
		return fmt.Errorf("error extracting links: %w", err)
	}
	if len(links) == 0 {
		return errors.New("no link found")
	}

	type doneItem struct {
		url      string
		updated  bool
		newLinks []string
	}
	done := make(chan doneItem, len(links))
	jobs := make(chan string, len(links))
	eg, ctx := errgroup.WithContext(ctx)
	for range numWorkers {
		eg.Go(func() error {
			for fullURL := range jobs {
				updated, newLinks, err3 := w.worker(ctx, fullURL)
				if err3 != nil {
					return err3
				}
				done <- doneItem{fullURL, updated, newLinks}
			}
			return nil
		})
	}
	seen := map[string]struct{}{}
	for _, url := range links {
		jobs <- url
		seen[url] = struct{}{}
	}
	total := len(links)
breakLoop:
	for processed := 0; processed < total; processed++ {
		select {
		case <-ctx.Done():
			break breakLoop
		case i := <-done:
			for _, l := range i.newLinks {
				if _, ok := seen[l]; !ok {
					seen[l] = struct{}{}
					total++
					select {
					case jobs <- l:
					case <-ctx.Done():
					}
				}
			}
			suffix := ""
			if i.updated {
				suffix = " (updated)"
			}
			fmt.Printf("- (%d/%d): %s%s\n", processed, total, i.url, suffix)
		}
	}

	close(jobs)
	err = eg.Wait()
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
	// targetURL is the URL to fetch links from
	const targetURL = "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z"
	fmt.Printf("Extracting links from %s\n", targetURL)
	if err = downloadAndSaveTexts(ctx, c, targetURL, *outputDir); err != nil {
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
