// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
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
type server struct{}

func (s *server) generateResponse(_ string) string {
	// For now, just return "Hello" as requested
	// This function will be replaced later with actual implementation
	return "Hello"
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

func (s *server) start() error {
	// Set up routes
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/api/chat", s.handleChat)

	port := "8080"
	log.Printf("Server starting on http://localhost:%s", port)
	return http.ListenAndServe(":"+port, nil)
}

func mainImpl() error {
	// Keep the original code for reference, but it won't be used directly in web server mode
	/*
		c, err := cerebras.New("", "llama-3.1-8b")
		if err != nil {
			return err
		}
		msgs := genai.Messages{
			genai.NewTextMessage(genai.User, "Is Ottawa great?"),
		}
		opts := genai.ChatOptions{
			Seed:        1,
			Temperature: 0.01,
			MaxTokens:   50,
		}
		ctx := context.Background()
		resp, err := c.Chat(ctx, msgs, &opts)
		if err != nil {
			return err
		}
		println(resp.Contents[0].Text)
	*/

	s := server{}
	return s.start()
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "citygpt: %s\n", err)
		os.Exit(1)
	}
}
