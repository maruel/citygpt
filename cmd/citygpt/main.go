// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/maruel/citygpt/data/ottawa"
	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a chat request from the client.
type ChatRequest struct {
	Message string `json:"message"`
}

// ChatResponse represents a response to the client.
type ChatResponse struct {
	Message Message `json:"message"`
}

//go:embed templates/chat.html
var htmlTemplate string

// server represents the HTTP server that handles the chat application.
type server struct {
	c        genai.ChatProvider
	cityData embed.FS
}

func (s *server) generateResponse(message string) string {
	msgs := genai.Messages{
		genai.NewTextMessage(genai.User, message),
	}
	opts := genai.ChatOptions{Seed: 1, Temperature: 0.01}
	ctx := context.Background()
	resp, err := s.c.Chat(ctx, msgs, &opts)
	if err != nil {
		slog.Error("Error generating response", "error", err)
		return "Sorry, there was an error processing your request."
	}
	if len(resp.Message.Contents) == 0 || resp.Message.Contents[0].Text == "" {
		return "No response generated"
	}
	return resp.Message.Contents[0].Text
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	response := s.generateResponse(req.Message)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ChatResponse{
		Message: Message{
			Role:    "assistant",
			Content: response,
		},
	})
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
	w.Write(data)
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	tmpl, err := template.New("chat").Parse(htmlTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.Error("Template parsing error", "error", err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	err = tmpl.Execute(w, nil)
	if err != nil {
		slog.Error("Template execution error", "error", err)
	}
}

func (s *server) start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/city-data/", s.handleCityData)
	mux.HandleFunc("/city-data", s.handleCityData)

	port := "8080"
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: time.Minute,
	}

	errorChan := make(chan error, 1)
	go func() {
		slog.Info("Server starting", "url", fmt.Sprintf("http://localhost:%s", port))
		errorChan <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		slog.Info("main", "message", "Shutdown signal received, gracefully shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}
		slog.Info("Server gracefully stopped")
		return nil
	case err := <-errorChan:
		return fmt.Errorf("server error: %w", err)
	}
}

func watchExecutable(cancel context.CancelFunc) error {
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
				slog.Warn("Could not stat executable", "error", err)
				continue
			}
			currentModTime := currentStat.ModTime()
			currentSize := currentStat.Size()
			if !currentModTime.Equal(initialModTime) || currentSize != initialSize {
				slog.Info("Executable file was modified, initiating shutdown...")
				cancel()
				break
			}
		}
	}()
	return nil
}

func mainImpl() error {
	Level := &slog.LevelVar{}
	Level.Set(slog.LevelInfo)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer cancel()
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
	if err := watchExecutable(cancel); err != nil {
		slog.Warn("Could not set up executable watcher", "error", err)
	}

	modelFlag := flag.String("model", "llama3.1-8b", "Model to use for chat completions")
	flag.Parse()

	c, err := cerebras.New("", "")
	if err == nil {
		if models, err2 := c.ListModels(ctx); err2 == nil && len(models) > 0 {
			modelNames := make([]string, 0, len(models))
			found := false
			for _, model := range models {
				n := model.GetID()
				if n == *modelFlag {
					found = true
					break
				}
				modelNames = append(modelNames, n)
			}
			if !found {
				return fmt.Errorf("bad model. Available models:\n  %s", strings.Join(modelNames, "\n  "))
			}
		}
	}
	c, err = cerebras.New("", *modelFlag)
	if err != nil {
		return err
	}
	s := server{
		c:        c,
		cityData: ottawa.DataFS,
	}
	return s.start(ctx)
}

func main() {
	if err := mainImpl(); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "citygpt: %s\n", err)
		os.Exit(1)
	}
}
