// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"os"
	"strings"
	"testing"
)

func TestStripHTMLAndJSONBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Plain text",
			input:    "This is just plain text",
			expected: "This is just plain text",
		},
		{
			name:     "HTML tags",
			input:    "<p>Text with HTML</p>",
			expected: "", // The stripHTMLAndJSONBlocks function only checks for standalone tags
		},
		{
			name:     "JSON object",
			input:    "Config: { \"name\": \"value\" }",
			expected: "Config: { \"name\": \"value\" }", // The JSON block isn't detected in a single line with text
		},
		{
			name:     "Standalone JSON object",
			input:    "{ \"name\": \"value\" }",
			expected: "", // This should be removed as a standalone JSON object
		},
		{
			name:     "Multi-line JSON",
			input:    "JSON block:\n{\n  \"key\": \"value\"\n}",
			expected: "JSON block:",
		},
		{
			name:     "Code with braces",
			input:    "Function: function test() { return true; }",
			expected: "Function: function test() { return true; }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripHTMLAndJSONBlocks(tt.input)
			result = strings.TrimSpace(result)
			tt.expected = strings.TrimSpace(tt.expected)

			if result != tt.expected {
				t.Errorf("stripHTMLAndJSONBlocks(%q):\ngot:  %q\nwant: %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractTextFromHTML(t *testing.T) {
	// Read the test HTML file
	testFile, err := os.Open("testdata/test_input.html")
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	defer testFile.Close()

	// Extract text from the test file
	extractedText, err := extractTextFromHTML(testFile)
	if err != nil {
		t.Fatalf("extractTextFromHTML failed: %v", err)
	}

	// Check that certain text was properly extracted
	expectedContents := []string{
		"By-Law No. 123-45",
		"This is a sample by-law text that should be extracted",
		"The following is an important section",
		"Item 1: Regulations about noise",
		"Item 2: Regulations about waste",
		"For more information, please see section 5.3 regarding enforcement",
	}

	for _, expected := range expectedContents {
		if !strings.Contains(extractedText, expected) {
			t.Errorf("Expected extracted text to contain %q, but it doesn't", expected)
		}
	}

	// Check that certain content was properly removed
	unwantedContents := []string{
		"<html>", "<body>", "<h1>", "<p>", "<pre>", "<code>",
		"function example()", "return \"Hello World\"",
		"\"bylaw\": \"123-45\"",
		"\"effective_date\": \"2025-01-01\"",
	}

	for _, unwanted := range unwantedContents {
		if strings.Contains(extractedText, unwanted) {
			t.Errorf("Expected extracted text to NOT contain %q, but it does", unwanted)
		}
	}
}
