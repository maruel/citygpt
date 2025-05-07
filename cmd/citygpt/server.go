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
	"sort"
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
	// Item is the selected file for the session.
	Item internal.Item `json:"item"`
	// Messages is the chat history for the session.
	Messages []Message `json:"messages"`
	// Created is when the session was created.
	Created time.Time `json:"created"`
	// Modified is when the session was last modified.
	Modified time.Time `json:"modified"`

	mu sync.Mutex
}

// Server

//go:embed templates
var templateFS embed.FS

// server represents the HTTP server that handles the chat application.
type server struct {
	// Immutable
	c             genai.ChatProvider
	cityData      fs.FS
	appName       string
	templates     map[string]*template.Template
	index         internal.Index
	files         map[string]internal.Item
	summaryPrompt string
	statePath     string          // statePath is the path to the state file on disk
	ipChecker     ipgeo.IPChecker // ipChecker is used to check if an IP is from Canada

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

// askLLMForBestFile asks the LLM which file would be the best source of data for answering the query.
//
// It retries up to 3 times with increasing temperature.
func (s *server) askLLMForBestFile(ctx context.Context, userMessage string) (internal.Item, error) {
	msgs := genai.Messages{
		genai.NewTextMessage(genai.User, "Here's a list of file names and their corresponding summary. The files contain information to answer the user's questions.:"),
		genai.NewTextMessage(genai.User, s.summaryPrompt),
		genai.NewTextMessage(genai.User, fmt.Sprintf("Using the previous summary information, determine which file would likely be the best source of information to answer this question. Please respond ONLY with the name of the single most relevant file.\n\nHere's the user's question: \"%s\"", userMessage)),
	}
	for attempt := range 3 {
		// Increase temperature with each attempt
		temperature := 0.1 * float64(attempt+1)
		slog.InfoContext(ctx, "citygpt", "msg", "Asking LLM for best file", "attempt", attempt+1, "temperature", temperature)
		opts := genai.ChatOptions{Seed: int64(attempt + 1), Temperature: temperature}
		resp, err := s.c.Chat(ctx, msgs, &opts)
		if err != nil {
			slog.WarnContext(ctx, "citygpt", "msg", "Error asking LLM for best file", "attempt", attempt+1, "err", err)
			continue
		}
		if len(resp.Contents) == 0 || resp.Contents[0].Text == "" {
			slog.WarnContext(ctx, "citygpt", "msg", "No response from LLM when asking for best file", "attempt", attempt+1)
			continue
		}
		response := strings.TrimSpace(resp.Message.Contents[0].Text)
		slog.InfoContext(ctx, "citygpt", "msg", "LLM suggested file", "file", response)
		if selected, ok := s.files[response]; ok {
			return selected, nil
		}
		slog.WarnContext(ctx, "citygpt", "msg", "LLM suggested invalid file", "file", response, "attempt", attempt+1)
	}

	return internal.Item{}, fmt.Errorf("failed to get a valid file after 3 attempts")
}

func (s *server) genericReply(ctx context.Context, message string, history []Message) string {
	msgs := make(genai.Messages, 0, len(history)+1)
	for _, msg := range history {
		msgs = append(msgs, genai.NewTextMessage(msg.Role, msg.Content))
	}
	msgs = append(msgs, genai.NewTextMessage(genai.User, message))

	opts := genai.ChatOptions{Seed: 1, Temperature: 0.1}
	resp, err := s.c.Chat(ctx, msgs, &opts)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Error generating response", "err", err)
		return "Sorry, there was an error processing your request."
	}
	if len(resp.Contents) == 0 || resp.Contents[0].Text == "" {
		return "No response generated"
	}
	return resp.Message.Contents[0].Text
}

func (s *server) generateResponse(ctx context.Context, msg string, sd *SessionData) string {
	msgs := make(genai.Messages, 0, len(sd.Messages)+1)
	for _, msg := range sd.Messages {
		msgs = append(msgs, genai.NewTextMessage(msg.Role, msg.Content))
	}
	var err error
	if len(msgs) > 1 {
		slog.InfoContext(ctx, "citygpt", "msg", "Follow up question", "file", sd.Item.Name, "msgs", len(msgs))
		msgs = append(msgs, genai.NewTextMessage(genai.User, msg))
	} else {
		if sd.Item, err = s.askLLMForBestFile(ctx, msg); err != nil {
			slog.ErrorContext(ctx, "citygpt", "msg", "Error asking LLM for best file", "err", err)
			return s.genericReply(ctx, msg, sd.Messages)
		}
		slog.InfoContext(ctx, "citygpt", "msg", "Selected best file for response", "file", sd.Item.Name)
		fileContent, err2 := fs.ReadFile(s.cityData, sd.Item.Name)
		if err2 != nil {
			slog.ErrorContext(ctx, "citygpt", "msg", "Error reading selected file", "file", sd.Item.Name, "err", err2)
			return s.genericReply(ctx, msg, sd.Messages)
		}
		prompt := fmt.Sprintf(
			"Using the following information from file '%s', please answer the user's questions in a concise way : \"%s\"\n\nFile content:\n%s",
			sd.Item.Name,
			msg,
			string(fileContent),
		)
		msgs = append(msgs, genai.NewTextMessage(genai.User, prompt))
	}

	opts := genai.ChatOptions{Seed: 1, Temperature: 0.1}
	resp, err := s.c.Chat(ctx, msgs, &opts)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Error generating response", "err", err)
		return "Sorry, there was an error processing your request."
	}
	if len(resp.Contents) == 0 || resp.Contents[0].Text == "" {
		return "No response generated"
	}
	return resp.Message.Contents[0].Text
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
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
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "Non-Canadian IP", "ip", clientIP, "country", countryCode)
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
	isNew := len(sd.Messages) == 0
	// Add the user's message to the history
	userMsg := Message{Role: "user", Content: req.Message}
	sd.Messages = append(sd.Messages, userMsg)
	resp := s.generateResponse(r.Context(), req.Message, sd)
	respMsg := Message{Role: "assistant", Content: resp}
	sd.Messages = append(sd.Messages, respMsg)
	sd.Modified = time.Now().Round(time.Second)

	// TODO: Run this asynchronously.
	// Save state after adding a new message.
	s.mu.Lock()
	if err := s.state.save(s.statePath); err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Failed to save state", "err", err)
	}
	s.mu.Unlock()

	if isNew {
		respMsg.Content = fmt.Sprintf("According to my understanding of <a href=\"%s\" target=\"_blank\"><i class=\"fa-solid fa-file-lines\"></i> %s</a>\n\n%s",
			html.EscapeString(sd.Item.URL),
			html.EscapeString(sd.Item.Title),
			html.EscapeString(resp))
	} else {
		respMsg.Content = html.EscapeString(resp)
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
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
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
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "Non-Canadian IP", "ip", clientIP, "country", countryCode)
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
			slog.WarnContext(ctx, "citygpt", "path", r.URL.Path, "msg", "Non-Canadian IP", "ip", clientIP, "country", countryCode)
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

func newServer(ctx context.Context, c genai.ChatProvider, appName string, files fs.FS) (*server, error) {
	s := &server{c: c, cityData: files, appName: appName, files: map[string]internal.Item{}}
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
	if err = s.index.Load(s.cityData, "index.json"); err != nil {
		return nil, err
	}
	content := make([]string, 0, len(s.index.Items))
	for _, item := range s.index.Items {
		s.files[item.Name] = item
		content = append(content, fmt.Sprintf("- %s: %s", item.Name, item.Summary))
	}
	sort.Strings(content)
	s.summaryPrompt = strings.Join(content, "\n")
	return s, nil
}

func (s *server) start(ctx context.Context, port string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /about", s.handleAbout)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("GET /city-data/", s.handleCityData)
	mux.HandleFunc("GET /city-data", s.handleCityData)

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
