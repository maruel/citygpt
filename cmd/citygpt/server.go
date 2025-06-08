// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/maruel/citygpt/internal"
	"github.com/maruel/citygpt/internal/ipgeo"
	"github.com/maruel/genai"
)

// APIs

// Message represents a chat message.
type Message struct {
	Role    genai.Role `json:"role"`
	Content string     `json:"content"`
}

// ChatRequest represents a chat request from the client.
type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
}

// ChatResponse represents a response to the client.
type ChatResponse struct {
	Message   Message `json:"message"`
	SessionID string  `json:"session_id,omitempty"`
}

// State

type State struct {
	Sessions map[string]*SessionData `json:"sessions"`
}

// load loads the server state from disk
func (s *State) load(ctx context.Context, p string) error {
	s.Sessions = map[string]*SessionData{}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		slog.InfoContext(ctx, "citygpt", "msg", "No existing state file found, starting with empty state", "path", p)
		return nil
	} else if err != nil {
		return fmt.Errorf("error checking state file: %w", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("error reading state file: %w", err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return fmt.Errorf("error parsing state file: %w", err)
	}
	slog.InfoContext(ctx, "citygpt", "msg", "Loaded state from disk", "sessions", len(s.Sessions), "path", p)
	return nil
}

// save saves the server state to disk
func (s *State) save(p string) error {
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return fmt.Errorf("error serializing state: %w", err)
	}
	// Write to temp file and rename for atomic replacement.
	tmpPath := p + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("error writing state file: %w", err)
	}
	if err := os.Rename(tmpPath, p); err != nil {
		return fmt.Errorf("error finalizing state file: %w", err)
	}
	return nil
}

// SessionData holds both the chat messages and selected file for a session.
type SessionData struct {
	// Citations are the selected file for the session.
	Citations []string `json:"citations,omitzero"`
	// Messages is the chat history for the session.
	Messages genai.Messages `json:"messages,omitzero"`
	// Created is when the session was created.
	Created time.Time `json:"created,omitzero"`
	// Modified is when the session was last modified.
	Modified time.Time `json:"modified,omitzero"`

	mu sync.Mutex
}

// Server

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// server represents the HTTP server that handles the chat application.
type server struct {
	// Immutable
	appName   string
	cityData  fs.FS
	files     map[string]internal.Item
	cityAgent cityAgent
	templates map[string]*template.Template
	statePath string          // statePath is the path to the state file on disk
	ipChecker ipgeo.IPChecker // ipChecker is used to check if an IP is from Canada

	// Mutable
	mu sync.Mutex
	// state stores both chat messages and selected files for each session
	state State
}

func generateSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b[:])
}

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientIP, err := ipgeo.GetRealIP(r)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Failed to determine client IP", "err", err)
		http.Error(w, "Can't determine IP address", http.StatusPreconditionFailed)
		return
	}
	// Check if the request is from Canada
	if s.ipChecker != nil {
		countryCode, err := s.ipChecker.GetCountry(clientIP)
		if err != nil {
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "Failed to check IP country code", "ip", clientIP, "err", err)
		} else if countryCode != "CA" && countryCode != "local" {
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "denied", "ip", clientIP, "country", countryCode)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "I'm sorry, I can only be used within Canada"}})
			return
		} else {
			slog.InfoContext(ctx, "citygpt", "path", r.URL.Path, "ip", clientIP, "country", countryCode)
		}
	} else {
		slog.InfoContext(ctx, "citygpt", "path", r.URL.Path, "ip", clientIP)
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		req.SessionID = generateSessionID()
	}
	s.mu.Lock()
	sd := s.state.Sessions[req.SessionID]
	if sd == nil {
		now := time.Now().Round(time.Second)
		sd = &SessionData{
			Created:  now,
			Modified: now,
		}
		s.state.Sessions[req.SessionID] = sd
	}
	s.mu.Unlock()
	sd.mu.Lock()
	defer sd.mu.Unlock()
	// Add the user's message to the history
	sd.Messages = append(sd.Messages, genai.NewTextMessage(genai.User, req.Message))
	newMsgs, files := s.cityAgent.query(r.Context(), sd.Messages)
	response := newMsgs[len(newMsgs)-1].AsText()
	sd.Messages = append(sd.Messages, newMsgs...)
	sd.Modified = time.Now().Round(time.Second)

	// TODO: Run this asynchronously.
	// Save state after adding a new message.
	s.mu.Lock()
	if err := s.state.save(s.statePath); err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Failed to save state", "err", err)
	}
	s.mu.Unlock()

	respMsg := Message{Role: "assistant"}
	if len(files) != 0 {
		str := ""
		for _, f := range files {
			item := s.files[f]
			str += fmt.Sprintf("<a href=\"%s\" target=\"_blank\"><i class=\"fa-solid fa-file-lines\"></i> %s</a>\n",
				html.EscapeString(item.URL),
				html.EscapeString(item.Title))
		}
		respMsg.Content = fmt.Sprintf("According to my understanding of %s\n\n%s",
			str, html.EscapeString(response))
	} else {
		respMsg.Content = html.EscapeString(response)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ChatResponse{Message: respMsg, SessionID: req.SessionID})
}

func (s *server) handleCityData(w http.ResponseWriter, r *http.Request) {
	// Extract the subpath from the URL
	subPath := strings.TrimPrefix(r.URL.Path, "/city-data")
	subPath = strings.TrimPrefix(subPath, "/")

	// If no subpath is provided, list all top-level files and directories
	if subPath == "" {
		// Get all entries in the embedded FS
		entries, err := fs.ReadDir(s.cityData, ".")
		if err != nil {
			http.Error(w, "Error reading data: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "Data Files:")
		for _, entry := range entries {
			fmt.Fprintf(w, "- %s\n", entry.Name())
			if !entry.IsDir() {
				data, err := fs.ReadDir(s.cityData, entry.Name())
				if err != nil {
					fmt.Fprintf(w, "  Error reading file: %v\n", err)
				} else {
					fmt.Fprintf(w, "  Size: %d bytes\n", len(data))
				}
			}
		}
		return
	}

	// Check if the path points to a directory
	if info, err := fs.Stat(s.cityData, subPath); err == nil && info.IsDir() {
		// If it's a directory, list its contents
		entries, err := fs.ReadDir(s.cityData, subPath)
		if err != nil {
			http.Error(w, "Error reading directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Contents of %s:\n", subPath)
		for _, entry := range entries {
			fullPath := path.Join(subPath, entry.Name())
			fmt.Fprintf(w, "- %s\n", entry.Name())
			if !entry.IsDir() {
				data, err := fs.ReadDir(s.cityData, fullPath)
				if err != nil {
					fmt.Fprintf(w, "  Error reading file: %v\n", err)
				} else {
					fmt.Fprintf(w, "  Size: %d bytes\n", len(data))
				}
			}
		}
		return
	}

	// Handle request for a specific file
	data, err := fs.ReadFile(s.cityData, subPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			http.Error(w, "Error reading file: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Set appropriate content type based on file extension
	contentType := "text/plain"
	if strings.HasSuffix(subPath, ".json") {
		contentType = "application/json"
	} else if strings.HasSuffix(subPath, ".html") {
		contentType = "text/html"
	} else if strings.HasSuffix(subPath, ".csv") {
		contentType = "text/csv"
	} else if strings.HasSuffix(subPath, ".xml") {
		contentType = "application/xml"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientIP, err := ipgeo.GetRealIP(r)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Failed to determine client IP", "err", err)
		http.Error(w, "Can't determine IP address", http.StatusPreconditionFailed)
		return
	}
	if s.ipChecker != nil {
		if countryCode, err := s.ipChecker.GetCountry(clientIP); err != nil {
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "Failed to check IP country code", "ip", clientIP, "err", err)
		} else if countryCode != "CA" && countryCode != "local" {
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "denied", "ip", clientIP, "country", countryCode)
		} else {
			slog.InfoContext(ctx, "citygpt", "path", r.URL.Path, "ip", clientIP, "country", countryCode)
		}
	} else {
		slog.InfoContext(ctx, "citygpt", "path", r.URL.Path, "ip", clientIP)
	}

	w.Header().Set("Content-Type", "text/html")
	// Pass the app name and current page to the template
	data := struct {
		AppName     string
		PageTitle   string
		HeaderTitle string
		CurrentPage string
	}{
		AppName:     s.appName,
		HeaderTitle: "Chat",
		CurrentPage: "chat",
	}
	if err := s.templates["/"].Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Template execution error", "err", err)
	}
}

func (s *server) handleAbout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientIP, err := ipgeo.GetRealIP(r)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Failed to determine client IP", "err", err)
		http.Error(w, "Can't determine IP address", http.StatusPreconditionFailed)
		return
	}
	if s.ipChecker != nil {
		if countryCode, err := s.ipChecker.GetCountry(clientIP); err != nil {
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "Failed to check IP country code", "ip", clientIP, "err", err)
		} else if countryCode != "CA" && countryCode != "local" {
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "denied", "ip", clientIP, "country", countryCode)
		} else {
			slog.InfoContext(ctx, "citygpt", "path", r.URL.Path, "ip", clientIP, "country", countryCode)
		}
	} else {
		slog.InfoContext(ctx, "citygpt", "path", r.URL.Path, "ip", clientIP)
	}

	w.Header().Set("Content-Type", "text/html")
	// Pass the app name and current page to the template
	data := struct {
		AppName     string
		PageTitle   string
		HeaderTitle string
		CurrentPage string
	}{
		AppName:     s.appName,
		PageTitle:   "About",
		HeaderTitle: "About",
		CurrentPage: "about",
	}
	if err := s.templates["/about"].Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Template execution error", "err", err)
	}
}

var trashPaths = []string{
	"/.aws/",
	"/.env",
	"/.git/",
	"/_profiler/",
	"/admin/",
	"/config.phpinfo",
	"/media/",
	"/php.php",
	"/php_info.php",
	"/phpinfo",
	"/phpinfo.php",
	"/pi.php",
	"/test.php",
	"/wordpress/",
	"/wp-admin/",
	"/wp-includes/",
}

func (s *server) handleTrash(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", http.StatusMovedPermanently)
}

func newServer(ctx context.Context, c genai.ProviderGen, appName string, files fs.FS) (*server, error) {
	s := &server{appName: appName, cityData: files}
	var err error
	if s.ipChecker, err = ipgeo.NewGeoIPChecker(); err != nil {
		slog.WarnContext(ctx, "citygpt", "msg", "Failed to initialize GeoIP database, IP restriction disabled", "err", err)
	}
	if err = s.compileTemplates(); err != nil {
		return s, err
	}
	configDir, err := internal.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine config directory: %w", err)
	}
	configDir = filepath.Join(configDir, "citygpt")
	if err = os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	s.statePath = filepath.Join(configDir, s.appName+".json")
	if err = s.state.load(ctx, s.statePath); err != nil {
		return nil, err
	}
	if err = s.cityAgent.init(c, files); err != nil {
		return nil, err
	}
	s.files = make(map[string]internal.Item, len(s.cityAgent.index.Items))
	for _, item := range s.cityAgent.index.Items {
		s.files[item.Name] = item
	}
	return s, nil
}

// noDirectoryFS is a wrapper around fs.FS that prevents directory listing by always
// returning an error for directories.
type noDirectoryFS struct {
	fs fs.FS
}

// Open implements fs.FS
func (n noDirectoryFS) Open(name string) (fs.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	// Return an error if this is a directory to prevent listing
	if stat.IsDir() {
		_ = f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}

func (s *server) start(ctx context.Context, port string) error {
	mux := http.NewServeMux()
	for _, t := range trashPaths {
		mux.HandleFunc("GET "+t, s.handleTrash)
	}
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /about", s.handleAbout)
	mux.HandleFunc("POST /api/chat", s.handleRoot)
	mux.HandleFunc("GET /city-data/", s.handleCityData)
	mux.HandleFunc("GET /city-data", s.handleCityData)

	// Serve static files without allowing directory listing
	staticSubFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("error setting up static file server: %w", err)
	}
	fileServer := http.FileServer(http.FS(noDirectoryFS{fs: staticSubFS}))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: time.Minute,
	}

	errorChan := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "citygpt", "msg", "Server starting", "url", fmt.Sprintf("http://localhost:%s", port))
		errorChan <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "citygpt", "msg", "Shutdown signal received, gracefully shutting down server...")
		s.mu.Lock()
		if err := s.state.save(s.statePath); err != nil {
			slog.ErrorContext(ctx, "citygpt", "msg", "Failed to save state during shutdown", "err", err)
		}
		s.mu.Unlock()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}
		return nil
	case err := <-errorChan:
		return fmt.Errorf("server error: %w", err)
	}
}

func (s *server) Close() error {
	if s.ipChecker != nil {
		if err := s.ipChecker.Close(); err != nil {
			slog.Warn("citygpt", "msg", "Failed to close GeoIP database", "err", err)
		}
	}
	return nil
}

func (s *server) compileTemplates() error {
	s.templates = map[string]*template.Template{}
	var err error
	if s.templates["/"], err = template.ParseFS(templateFS, "templates/layout.html", "templates/index.html"); err != nil {
		return err
	}
	if s.templates["/about"], err = template.ParseFS(templateFS, "templates/layout.html", "templates/about.html"); err != nil {
		return err
	}
	return nil
}
