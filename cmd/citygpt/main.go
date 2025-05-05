// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/citygpt"
	"github.com/maruel/citygpt/data/ottawa"
	"github.com/maruel/citygpt/internal"
	"github.com/maruel/citygpt/internal/ipgeo"
	"github.com/maruel/genai"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

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

//go:embed templates/layout.html
var layoutHTMLTemplate string

//go:embed templates/chat.html
var chatHTMLTemplate string

//go:embed templates/about.html
var aboutHTMLTemplate string

type State struct {
	Sessions map[string]*SessionData `json:"sessions"`
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

// server represents the HTTP server that handles the chat application.
type server struct {
	c        genai.ChatProvider
	cityData citygpt.ReadDirFileFS
	appName  string

	files   map[string]internal.Item
	summary string

	// state stores both chat messages and selected files for each session
	state     State
	stateLock sync.Mutex

	// statePath is the path to the state file on disk
	statePath string

	// ipChecker is used to check if an IP is from Canada
	ipChecker ipgeo.IPChecker
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
	prompt := fmt.Sprintf(
		"Given the user's question: \"%s\"\n\nUsing the following summary information, which file would likely be the best source of information to answer this question? Please respond ONLY with the name of the single most relevant file.\n\nSummary information:\n%s",
		userMessage,
		s.summary,
	)
	for attempt := range 3 {
		// Increase temperature with each attempt
		temperature := 0.1 * float64(attempt+1)
		slog.InfoContext(ctx, "citygpt", "msg", "Asking LLM for best file", "attempt", attempt+1, "temperature", temperature)
		msgs := genai.Messages{genai.NewTextMessage(genai.User, prompt)}
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
	if len(msgs) > 0 {
		slog.InfoContext(ctx, "citygpt", "msg", "Follow up question", "file", sd.Item.Name)
		msgs = append(msgs, genai.NewTextMessage(genai.User, msg))
	} else {
		if sd.Item, err = s.askLLMForBestFile(ctx, msg); err != nil {
			slog.ErrorContext(ctx, "citygpt", "msg", "Error asking LLM for best file", "err", err)
			return s.genericReply(ctx, msg, sd.Messages)
		}
		slog.InfoContext(ctx, "citygpt", "msg", "Selected best file for response", "file", sd.Item.Name)
		fileContent, err2 := s.cityData.ReadFile(sd.Item.Name)
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
			slog.WarnContext(ctx, "citygpt", "msg", "Failed to check IP country code", "ip", clientIP, "err", err)
		} else if countryCode != "CA" && countryCode != "local" {
			slog.InfoContext(ctx, "citygpt", "msg", "Blocked non-Canadian IP", "ip", clientIP, "country", countryCode)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "I'm sorry, I can only be used within Canada"}})
			return
		} else {
			slog.InfoContext(ctx, "citygpt", "ip", clientIP, "country", countryCode)
		}
	} else {
		slog.InfoContext(ctx, "citygpt", "ip", clientIP)
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		req.SessionID = generateSessionID()
	}
	s.stateLock.Lock()
	sd := s.state.Sessions[req.SessionID]
	if sd == nil {
		now := time.Now()
		sd = &SessionData{
			Created:  now,
			Modified: now,
		}
		s.state.Sessions[req.SessionID] = sd
	}
	s.stateLock.Unlock()
	sd.mu.Lock()
	defer sd.mu.Unlock()
	isNew := len(sd.Messages) == 0
	// Add the user's message to the history
	userMsg := Message{Role: "user", Content: req.Message}
	sd.Messages = append(sd.Messages, userMsg)
	resp := s.generateResponse(r.Context(), req.Message, sd)
	respMsg := Message{Role: "assistant", Content: resp}
	sd.Messages = append(sd.Messages, respMsg)
	sd.Modified = time.Now()

	// TODO: Run this asynchronously.
	// Save state after adding a new message.
	s.stateLock.Lock()
	if err := s.saveState(); err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Failed to save state", "err", err)
	}
	s.stateLock.Unlock()

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
		entries, err := s.cityData.ReadDir(".")
		if err != nil {
			http.Error(w, "Error reading data: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "Data Files:")
		for _, entry := range entries {
			fmt.Fprintf(w, "- %s\n", entry.Name())
			if !entry.IsDir() {
				data, err := s.cityData.ReadFile(entry.Name())
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
		entries, err := s.cityData.ReadDir(subPath)
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
				data, err := s.cityData.ReadFile(fullPath)
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
	data, err := s.cityData.ReadFile(subPath)
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

	// Create a template with both layout and chat templates
	tmpl, err := template.New("layout").Parse(layoutHTMLTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.ErrorContext(ctx, "citygpt", "msg", "Layout template parsing error", "err", err)
		return
	}

	// Parse the chat template
	_, err = tmpl.Parse(chatHTMLTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.ErrorContext(ctx, "citygpt", "msg", "Chat template parsing error", "err", err)
		return
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
	err = tmpl.Execute(w, data)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Template execution error", "err", err)
	}
}

func (s *server) handleAbout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Create a template with both layout and about templates
	tmpl, err := template.New("layout").Parse(layoutHTMLTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.ErrorContext(ctx, "citygpt", "msg", "Layout template parsing error", "err", err)
		return
	}

	// Parse the about template
	_, err = tmpl.Parse(aboutHTMLTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.ErrorContext(ctx, "citygpt", "msg", "About template parsing error", "err", err)
		return
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
	err = tmpl.Execute(w, data)
	if err != nil {
		slog.ErrorContext(ctx, "citygpt", "msg", "Template execution error", "err", err)
	}
}

// loadState loads the server state from disk
func (s *server) loadState(ctx context.Context) error {
	s.state.Sessions = map[string]*SessionData{}
	if _, err := os.Stat(s.statePath); os.IsNotExist(err) {
		slog.InfoContext(ctx, "citygpt", "msg", "No existing state file found, starting with empty state", "path", s.statePath)
		return nil
	} else if err != nil {
		return fmt.Errorf("error checking state file: %w", err)
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return fmt.Errorf("error reading state file: %w", err)
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return fmt.Errorf("error parsing state file: %w", err)
	}
	slog.InfoContext(ctx, "citygpt", "msg", "Loaded state from disk", "sessions", len(s.state.Sessions), "path", s.statePath)
	return nil
}

// saveState saves the server state to disk
func (s *server) saveState() error {
	data, err := json.MarshalIndent(s.state, "", " ")
	if err != nil {
		return fmt.Errorf("error serializing state: %w", err)
	}
	// Write to temp file and rename for atomic replacement.
	tmpPath := s.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("error writing state file: %w", err)
	}
	if err := os.Rename(tmpPath, s.statePath); err != nil {
		return fmt.Errorf("error finalizing state file: %w", err)
	}
	return nil
}

func (s *server) start(ctx context.Context, port string) error {
	var err error
	if s.ipChecker, err = ipgeo.NewGeoIPChecker(); err != nil {
		slog.WarnContext(ctx, "citygpt", "msg", "Failed to initialize GeoIP database, IP restriction disabled", "err", err)
	} else {
		defer func() {
			if err2 := s.ipChecker.Close(); err2 != nil {
				slog.WarnContext(ctx, "citygpt", "msg", "Failed to close GeoIP database", "err", err2)
			}
		}()
	}
	configDir, err := internal.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to determine config directory: %w", err)
	}
	configDir = filepath.Join(configDir, "citygpt")
	if err = os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	s.statePath = filepath.Join(configDir, s.appName+".json")
	if err = s.loadState(ctx); err != nil {
		return err
	}
	raw, err := s.cityData.ReadFile("index.json")
	if err != nil {
		return err
	}
	data := internal.Index{}
	if err = json.Unmarshal(raw, &data); err != nil {
		return err
	}
	s.files = map[string]internal.Item{}
	var content []string
	for _, item := range data.Items {
		s.files[item.Name] = item
		content = append(content, fmt.Sprintf("- %s: %s", item.Name, item.Summary))
	}
	sort.Strings(content)
	s.summary = strings.Join(content, "\n")

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
		s.stateLock.Lock()
		if err := s.saveState(); err != nil {
			slog.ErrorContext(ctx, "citygpt", "msg", "Failed to save state during shutdown", "err", err)
		}
		s.stateLock.Unlock()
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

func watchExecutable(ctx context.Context, cancel context.CancelFunc) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	initialStat, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("failed to stat executable: %w", err)
	}
	initialModTime := initialStat.ModTime()
	initialSize := initialStat.Size()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			currentStat, err := os.Stat(exePath)
			if err != nil {
				slog.WarnContext(ctx, "citygpt", "msg", "Could not stat executable", "err", err)
				continue
			}
			currentModTime := currentStat.ModTime()
			currentSize := currentStat.Size()
			if !currentModTime.Equal(initialModTime) || currentSize != initialSize {
				slog.InfoContext(ctx, "citygpt", "msg", "Executable file was modified, initiating shutdown...")
				cancel()
				break
			}
		}
	}()
	return nil
}

func mainImpl() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
	Level := &slog.LevelVar{}
	Level.Set(slog.LevelDebug)
	logger := slog.New(tint.NewHandler(colorable.NewColorable(os.Stderr), &tint.Options{
		Level:      Level,
		TimeFormat: "15:04:05.000", // Like time.TimeOnly plus milliseconds.
		NoColor:    !isatty.IsTerminal(os.Stderr.Fd()),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch t := a.Value.Any().(type) {
			case string:
				if t == "" {
					return slog.Attr{}
				}
			case bool:
				if !t {
					return slog.Attr{}
				}
			case uint64:
				if t == 0 {
					return slog.Attr{}
				}
			case int64:
				if t == 0 {
					return slog.Attr{}
				}
			case float64:
				if t == 0 {
					return slog.Attr{}
				}
			case time.Time:
				if t.IsZero() {
					return slog.Attr{}
				}
			case time.Duration:
				if t == 0 {
					return slog.Attr{}
				}
			}
			return a
		},
	}))
	slog.SetDefault(logger)
	if err := watchExecutable(ctx, cancel); err != nil {
		return err
	}

	appName := flag.String("app-name", "OttawaGPT", "The name of the application displayed in the UI")
	port := flag.String("port", "8080", "The port to run the server on")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unsupported argument")
	}
	c, err := internal.LoadProvider(ctx)
	if err != nil {
		return err
	}
	s := server{c: c, cityData: ottawa.DataFS, appName: *appName}
	return s.start(ctx, *port)
}

func main() {
	if err := mainImpl(); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "citygpt: %s\n", err)
		os.Exit(1)
	}
}
