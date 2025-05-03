// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/maruel/genai"
	"github.com/maruel/genai/cerebras"
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

// HTML template for the chat interface.
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>CityGPT Chat</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
            display: flex;
            flex-direction: column;
            height: 100vh;
        }
        #chat-container {
            flex-grow: 1;
            overflow-y: auto;
            margin-bottom: 15px;
            border: 1px solid #ccc;
            border-radius: 5px;
            padding: 15px;
        }
        .message {
            margin-bottom: 10px;
            padding: 8px 12px;
            border-radius: 18px;
            max-width: 70%;
            word-wrap: break-word;
        }
        .user-message {
            background-color: #e1f5fe;
            align-self: flex-end;
            margin-left: auto;
        }
        .assistant-message {
            background-color: #f5f5f5;
            align-self: flex-start;
        }
        #input-container {
            display: flex;
            gap: 10px;
        }
        #message-input {
            flex-grow: 1;
            padding: 10px;
            border: 1px solid #ccc;
            border-radius: 5px;
        }
        #send-button {
            padding: 10px 15px;
            background-color: #0084ff;
            color: white;
            border: none;
            border-radius: 5px;
            cursor: pointer;
        }
        #send-button:hover {
            background-color: #0073e6;
        }
    </style>
</head>
<body>
    <h1>CityGPT Chat</h1>
    <div id="chat-container">
        <!-- Chat messages will appear here -->
    </div>
    <div id="input-container">
        <input type="text" id="message-input" placeholder="Type your message..." autofocus>
        <button id="send-button">Send</button>
    </div>

    <script>
        const chatContainer = document.getElementById('chat-container');
        const messageInput = document.getElementById('message-input');
        const sendButton = document.getElementById('send-button');

        // Add event listeners
        sendButton.addEventListener('click', sendMessage);
        messageInput.addEventListener('keypress', function(e) {
            if (e.key === 'Enter') {
                sendMessage();
            }
        });

        // Function to send message
        function sendMessage() {
            const message = messageInput.value.trim();
            if (message === '') return;

            // Add user message to chat
            addMessageToChat('user', message);
            messageInput.value = '';

            // Send message to server
            fetch('/api/chat', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ message }),
            })
            .then(response => response.json())
            .then(data => {
                // Add response to chat
                addMessageToChat(data.message.role, data.message.content);
            })
            .catch(error => {
                console.error('Error:', error);
                addMessageToChat('assistant', 'Sorry, there was an error processing your request.');
            });
        }

        // Function to add message to chat
        function addMessageToChat(role, content) {
            const messageElement = document.createElement('div');
            messageElement.classList.add('message');
            messageElement.classList.add(role === 'user' ? 'user-message' : 'assistant-message');
            messageElement.textContent = content;
            chatContainer.appendChild(messageElement);
            chatContainer.scrollTop = chatContainer.scrollHeight;
        }
    </script>
</body>
</html>
`

// server represents the HTTP server that handles the chat application.
type server struct {
	c genai.ChatProvider
}

func (s *server) generateResponse(message string) string {
	msgs := genai.Messages{
		genai.NewTextMessage(genai.User, message),
	}
	opts := genai.ChatOptions{Seed: 1, Temperature: 0.01}
	ctx := context.Background()
	resp, err := s.c.Chat(ctx, msgs, &opts)
	if err != nil {
		log.Printf("Error generating response: %v", err)
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
	json.NewEncoder(w).Encode(ChatResponse{
		Message: Message{
			Role:    "assistant",
			Content: response,
		},
	})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	tmpl, err := template.New("chat").Parse(htmlTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		log.Printf("Template parsing error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	err = tmpl.Execute(w, nil)
	if err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

func (s *server) start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/chat", s.handleChat)

	port := "8080"
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	errorChan := make(chan error, 1)
	go func() {
		log.Printf("Server starting on http://localhost:%s", port)
		errorChan <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received, gracefully shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}
		log.Println("Server gracefully stopped")
		return nil
	case err := <-errorChan:
		return fmt.Errorf("server error: %w", err)
	}
}

// watchExecutable watches for changes to the executable and signals for a shutdown when detected.
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
				log.Printf("Warning: Could not stat executable: %v", err)
				continue
			}

			currentModTime := currentStat.ModTime()
			currentSize := currentStat.Size()

			if !currentModTime.Equal(initialModTime) || currentSize != initialSize {
				log.Println("Executable file was modified, initiating shutdown...")
				cancel()
				break
			}
		}
	}()

	return nil
}

func mainImpl() error {
	// Define flags
	modelFlag := flag.String("model", "llama-3.1-8b", "Model to use for chat completions")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %s", sig)
		cancel()
	}()

	// Set up executable file watcher
	if err := watchExecutable(cancel); err != nil {
		log.Printf("Warning: Could not set up executable watcher: %v", err)
	}

	c, err := cerebras.New("", "")
	if err == nil {
		models, err := c.ListModels(ctx)
		if err == nil && len(models) > 0 {
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
	s := server{c: c}
	return s.start(ctx)
}

func main() {
	if err := mainImpl(); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "citygpt: %s\n", err)
		os.Exit(1)
	}
}
