// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTextFromHTML(t *testing.T) {
	testFiles, err := filepath.Glob("testdata/*.html")
	if err != nil {
		t.Fatalf("Failed to list test files: %v", err)
	}
	if len(testFiles) == 0 {
		t.Fatal("No test HTML files found in testdata directory")
	}
	for _, testFile := range testFiles {
		t.Run(testFile, func(t *testing.T) {
			f, err := os.Open(testFile)
			if err != nil {
				t.Fatalf("Failed to open test file: %v", err)
			}
			defer f.Close()
			textContent, err := extractTextFromHTML(f)
			if err != nil {
				t.Fatalf("Failed to extract text: %v", err)
			}
			if len(textContent) == 0 {
				t.Error("Extracted text is empty")
			}
			if strings.Contains(textContent, "<div") || strings.Contains(textContent, "</div>") {
				t.Error("Extracted text contains HTML tags")
			}
			if strings.Contains(textContent, "{\"json\"") {
				t.Error("Extracted text contains JSON-like content")
			}
			t.Logf("Successfully extracted %d characters of text", len(textContent))
		})
	}
}
