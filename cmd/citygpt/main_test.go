// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"context"
	"testing"

	"github.com/maruel/genai"
)

// mockChatProvider is a simple implementation of genai.ChatProvider for testing purposes
type mockChatProvider struct{}

func (m *mockChatProvider) Chat(ctx context.Context, msgs genai.Messages, opts genai.Validatable) (genai.ChatResult, error) {
	mockContent := genai.Content{
		Text: "This is a mock response from CityGPT",
	}
	return genai.ChatResult{
		Message: genai.Message{
			Role:     genai.Assistant,
			Contents: []genai.Content{mockContent},
		},
	}, nil
}

func (m *mockChatProvider) ChatStream(ctx context.Context, msgs genai.Messages, opts genai.Validatable, replies chan<- genai.MessageFragment) error {
	// Implementation not needed for this example
	return nil
}

// TestMockChatProvider verifies that the mock provider works as expected
func TestMockChatProvider(t *testing.T) {
	// Create a mock provider
	mockProvider := &mockChatProvider{}

	// Set up a test request
	msgs := genai.Messages{
		genai.NewTextMessage(genai.User, "Test message"),
	}
	opts := genai.ChatOptions{}

	// Call the Chat method
	result, err := mockProvider.Chat(context.Background(), msgs, &opts)
	// Verify there was no error
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify the response content
	if len(result.Message.Contents) == 0 {
		t.Fatal("Expected response to have contents, got none")
	}

	// Verify the text of the response
	expected := "This is a mock response from CityGPT"
	actual := result.Message.Contents[0].Text
	if actual != expected {
		t.Fatalf("Expected response text '%s', got '%s'", expected, actual)
	}
}
