// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

// data-ingestor extracts and downloads text content from a city website.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/citygpt/internal"
	"github.com/maruel/citygpt/internal/htmlparse"
	"github.com/maruel/genai"
	"github.com/maruel/roundtrippers"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	htmlMinify "github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
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

// extractLinks extracts links that are rooted at baseURL from an HTML page in r at url u.
//
// baseURL and u must be full URLs.
func extractLinks(baseURL, u string, r io.Reader) ([]string, error) {
	// baseURL is the base URL for resolving relative links
	parsedURL, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
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
	// Normally web server are case sensitive, except Windows.
	if !strings.HasPrefix(strings.ToLower(link), strings.ToLower(baseURL)) {
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
	client        http.Client
	c             genai.ProviderGen
	outputDir     string
	previousIndex internal.Index
	urlLookup     map[string]int

	mu       sync.Mutex
	newIndex internal.Index
}

func urlToMDName(baseURL, targetURL string) (string, error) {
	// TODO: Repetitive work.
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", targetURL, err)
	}
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", targetURL, err)
	}
	// Normally web server are case sensitive, except Windows.
	if !strings.HasPrefix(strings.ToLower(parsedURL.Path), strings.ToLower(parsedBaseURL.Path)) {
		return "", fmt.Errorf("url %s is not a subpath of %s", targetURL, baseURL)
	}
	md := url.PathEscape(strings.TrimSuffix(targetURL[len(baseURL):], "/"))
	if md == "" {
		md = "index"
	}
	return md + ".md", nil
}

func (s *summaryWorkers) worker(ctx context.Context, baseURL, urlToVisit string) (bool, []string, error) {
	// Always download a fresh copy of the HTML page in case it changed.
	resp, err := s.client.Get(urlToVisit)
	if err != nil {
		return false, nil, fmt.Errorf("failed to fetch %s: %w", urlToVisit, err)
	}
	// Read the page in memory because we parse it twice.
	htmlBodyRaw, err := io.ReadAll(resp.Body)
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
		return false, nil, fmt.Errorf("received non-200 response for %s: %d", urlToVisit, resp.StatusCode)
	}

	mdName, err := urlToMDName(baseURL, urlToVisit)
	if err != nil {
		return false, nil, err
	}
	mdPath := filepath.Join(s.outputDir, mdName)
	htmlPath := filepath.Join(s.outputDir, strings.TrimSuffix(mdName, ".md")+".html")
	if err = os.MkdirAll(filepath.Dir(mdPath), 0o755); err != nil {
		return false, nil, err
	}

	// Compact the HTML first.
	m := minify.New()
	m.AddFunc("text/html", htmlMinify.Minify)
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
	m.AddFuncRegexp(regexp.MustCompile("[/+]json$"), json.Minify)
	m.AddFuncRegexp(regexp.MustCompile("[/+]xml$"), xml.Minify)
	m.URL, _ = url.Parse(urlToVisit)
	htmlMin := bytes.Buffer{}
	if err := m.Minify("text/html", &htmlMin, bytes.NewReader(htmlBodyRaw)); err != nil {
		return false, nil, err
	}
	if err = os.WriteFile(htmlPath, htmlMin.Bytes(), 0o644); err != nil {
		return false, nil, err
	}
	// Then extract links.
	links, err := extractLinks(baseURL, urlToVisit, bytes.NewReader(htmlMin.Bytes()))
	if err != nil {
		return false, links, err
	}
	// Then extract markdown.
	md, title, err := htmlparse.ExtractTextFromHTML(bytes.NewReader(htmlMin.Bytes()))
	if err != nil {
		return false, links, fmt.Errorf("failed to extract text: %w", err)
	}

	now := time.Now().Round(time.Second)
	created := now
	if i, ok := s.urlLookup[urlToVisit]; ok {
		prev := s.previousIndex.Items[i]
		if prev.Name == mdName && prev.Title == title {
			// We saw this page in a previous run.
			if !prev.Created.IsZero() {
				created = prev.Created
			}
			// We don't care if the HTML changed if the resulting markdown is the same.
			if oldMD, err2 := os.ReadFile(mdPath); err2 == nil && string(oldMD) == md {
				// Markdown content is the same
				// No need to re-create the summary, the content didn't change.
				s.mu.Lock()
				s.newIndex.Items = append(s.newIndex.Items, prev)
				s.mu.Unlock()
				return false, links, nil
			}
		}
	}

	// Content changed or is new, create both markdown and HTML files.
	if err = os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return false, links, err
	}

	item := internal.Item{
		URL:      urlToVisit,
		Title:    title,
		Name:     mdName,
		Created:  created,
		Modified: now,
		Model:    s.c.ModelID(),
	}
	item.Summary, err = internal.Summarize(ctx, s.c, md)
	if err != nil {
		return false, links, err
	}
	s.mu.Lock()
	s.newIndex.Items = append(s.newIndex.Items, item)
	s.mu.Unlock()
	return true, links, nil
}

type dataIngestor struct {
	// startURL is the URL to fetch links from.
	startURL string
	// baseURL is the base URL of the content we care about. Generally under targetURL but not always.
	baseURL string
}

// downloadAndSaveTexts downloads content from links and saves the text using 8 workers in parallel
func (d *dataIngestor) downloadAndSaveTexts(ctx context.Context, c genai.ProviderGen, outputDir string) (*internal.Index, error) {
	// Number of workers to process URLs and generate summaries in parallel. Generating summaries is slow so it
	// needs to be significantly higher than 1/qps.
	const numWorkers = 16
	// Maximum queries per second to hit the HTTP server with HTTP GET. We don't want them to hate us.
	const qps = 1.

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	client := http.Client{
		Transport: &roundtrippers.Header{
			Header: http.Header{"User-Agent": {"CityGPT"}},
			Transport: &roundtrippers.Retry{
				// Throttle retries too.
				Transport: &roundtrippers.Throttle{
					QPS:       qps,
					Transport: http.DefaultTransport,
				},
			},
		},
	}
	now := time.Now().Round(time.Second)
	w := summaryWorkers{client: client, c: c, outputDir: outputDir, urlLookup: map[string]int{}, newIndex: internal.Index{Version: 1, Created: now, Modified: now}}
	if err := w.previousIndex.Load(os.DirFS(outputDir), "index.json"); err != nil {
		return nil, err
	}
	if len(w.previousIndex.Items) > 0 && !w.previousIndex.Created.IsZero() {
		w.newIndex.Created = w.previousIndex.Created
	}
	for i := range w.previousIndex.Items {
		w.urlLookup[w.previousIndex.Items[i].URL] = i
	}

	// Start with the first page.
	resp, err := client.Get(d.startURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}
	links, err := extractLinks(d.baseURL, d.startURL, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("error extracting links: %w", err)
	}
	if len(links) == 0 {
		return nil, errors.New("no link found")
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
			for urlToVisit := range jobs {
				updated, newLinks, err3 := w.worker(ctx, d.baseURL, urlToVisit)
				if err3 != nil {
					return err3
				}
				done <- doneItem{urlToVisit, updated, newLinks}
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
	for processed := range total {
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
	if err2 := w.newIndex.Save(filepath.Join(outputDir, "index.json")); err == nil {
		err = err2
	}
	out := &internal.Index{}
	*out = w.newIndex
	return out, err
}

// cleanupOutputDir removes files from outputDir that are not listed in the index.
func cleanupOutputDir(outputDir string, index *internal.Index) error {
	validFiles := make(map[string]struct{}, len(index.Items)+1)
	for _, item := range index.Items {
		validFiles[item.Name] = struct{}{}
		// Also add the corresponding HTML file
		validFiles[strings.TrimSuffix(item.Name, ".md")+".html"] = struct{}{}
	}
	validFiles["index.json"] = struct{}{}
	return filepath.WalkDir(outputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		if _, exists := validFiles[relPath]; !exists {
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Printf("Deleted file not in index: %s\n", relPath)
		}
		return nil
	})
}

var cities = map[string]dataIngestor{
	"ottawa": {
		startURL: "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z",
		baseURL:  "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z" + "/",
	},
	"gatineau": {
		// Requires PDF support.
		startURL: "https://www.gatineau.ca/portail/default.aspx?p=guichet_municipal/reglements_municipaux",
		baseURL:  "https://docweb.gatineau.ca/Doc-Web/masson/documents/pdf/",
	},
}

func throttler(r http.RoundTripper) http.RoundTripper {
	return &roundtrippers.Throttle{
		QPS:       1,
		Transport: r,
	}
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	Level := &slog.LevelVar{}
	Level.Set(slog.LevelInfo)
	logger := slog.New(tint.NewHandler(colorable.NewColorable(os.Stderr), &tint.Options{
		Level:      Level,
		TimeFormat: "15:04:05.000", // Like time.TimeOnly plus milliseconds.
		NoColor:    !isatty.IsTerminal(os.Stderr.Fd()),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			val := a.Value.Any()
			skip := false
			switch t := val.(type) {
			case string:
				skip = t == ""
			case bool:
				skip = !t
			case uint64:
				skip = t == 0
			case int64:
				skip = t == 0
			case float64:
				skip = t == 0
			case time.Time:
				skip = t.IsZero()
			case time.Duration:
				skip = t == 0
			case nil:
				skip = true
			}
			if skip {
				return slog.Attr{}
			}
			return a
		},
	}))
	slog.SetDefault(logger)
	names := internal.ListProviderGen()

	outputDir := flag.String("output-dir", "", "Directory to save downloaded markdown files; defaults to data/<city>/ingested")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	city := flag.String("city", "", "City to fetch from, one of ottawa, gatineau")
	provider := flag.String("provider", "", "backend to use: "+strings.Join(names, ", "))
	remote := flag.String("remote", "", "URL to use, useful for local backend")
	model := flag.String("model", "", "model to use, defaults to a good model; use either the model ID or PREFERRED_CHEAP and PREFERRED_SOTA to automatically select cheaper or better models")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unknown arguments")
	}
	if *verbose {
		Level.Set(slog.LevelDebug)
	}
	cs := cities[*city]
	if cs.startURL == "" {
		return fmt.Errorf("unknown city: %s", *city)
	}
	if *outputDir == "" {
		*outputDir = filepath.Join("data", *city, "ingested")
	}
	wrapper := throttler
	if *remote != "" {
		// Assume that if we use a local model, we don't need to throttle.
		wrapper = nil
	}
	c, err := internal.LoadProviderGen(ctx, *provider, &genai.ProviderOptions{Remote: *remote, Model: *model}, wrapper)
	if err != nil {
		return err
	}

	fmt.Printf("Extracting links from %s\n", cs.startURL)
	index, err := cs.downloadAndSaveTexts(ctx, c, *outputDir)
	if err != nil {
		return fmt.Errorf("error downloading texts: %w", err)
	}
	if err = cleanupOutputDir(*outputDir, index); err != nil {
		return fmt.Errorf("error during file cleanup: %w", err)
	}

	fmt.Println("Process completed successfully")
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "data-ingestor: %s\n", err)
		os.Exit(1)
	}
}
