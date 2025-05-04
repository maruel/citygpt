// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package htmlparse

import (
	"os"
	"strings"
	"testing"
)

func TestExtractTextFromHTML(t *testing.T) {
	t.Run("with block-mainpagecontent", func(t *testing.T) {
		// Test with a file that has a block-mainpagecontent div
		testFile, err := os.Open("testdata/test_mainpagecontent.html")
		if err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		defer testFile.Close()

		text, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Check that main content is included
		if !strings.Contains(text, "Main Content") {
			t.Error("Expected 'Main Content' to be extracted")
		}
		if !strings.Contains(text, "This is the main content of the page") {
			t.Error("Expected main content text to be extracted")
		}

		// Check that content outside the main content div is NOT included
		if strings.Contains(text, "Page Header") {
			t.Error("Content outside main content div should not be extracted")
		}
		if strings.Contains(text, "This is outside the main content area") {
			t.Error("Content outside main content div should not be extracted")
		}
	})

	t.Run("without block-mainpagecontent", func(t *testing.T) {
		// Test with a file that doesn't have a block-mainpagecontent div
		testFile, err := os.Open("testdata/test_footer.html")
		if err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		defer testFile.Close()

		text, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Check that content is included
		if !strings.Contains(text, "Main Content") {
			t.Error("Expected 'Main Content' to be extracted")
		}
		if !strings.Contains(text, "This is the main content of the page") {
			t.Error("Expected main content text to be extracted")
		}

		// Since we're not filtering by div with block-mainpagecontent,
		// we should see all content
		if !strings.Contains(text, "This is footer content") {
			t.Error("Expected footer content to be extracted")
		}
		if !strings.Contains(text, "This is after the footer") {
			t.Error("Expected content after footer to be extracted")
		}
	})
}

func TestStripHTMLAndJSONBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HTML tag line",
			input:    "Some text\n<div></div>",
			expected: "Some text",
		},
		{
			name:     "JSON block",
			input:    "Text before\n{\"key\": \"value\"}\nText after",
			expected: "Text before\nText after",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := StripHTMLAndJSONBlocks(tc.input)
			result = strings.TrimSpace(result)
			expected := strings.TrimSpace(tc.expected)
			if result != expected {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
			}
		})
	}
}
