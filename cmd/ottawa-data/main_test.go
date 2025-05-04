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

// Golden file tests for HTML processing

// TestHTMLProcessingWithGolden verifies that HTML files are processed correctly
// by comparing the output with golden files.
func TestHTMLProcessingWithGolden(t *testing.T) {
	// Find all HTML test files
	testFiles, err := filepath.Glob("testdata/*.html")
	if err != nil {
		t.Fatalf("Failed to list test files: %v", err)
	}
	if len(testFiles) == 0 {
		t.Fatal("No test HTML files found in testdata directory")
	}

	// Process each file and compare with golden
	for _, testFile := range testFiles {
		t.Run(filepath.Base(testFile), func(t *testing.T) {
			// Determine golden file path
			goldenFile := testFile + ".golden"

			// Open and process the HTML file
			f, err := os.Open(testFile)
			if err != nil {
				t.Fatalf("Failed to open test file: %v", err)
			}

			// Extract text from HTML
			textContent, err := extractTextFromHTML(f)
			f.Close()
			if err != nil {
				t.Fatalf("Failed to extract text: %v", err)
			}

			// Golden files should be generated using the download_test_page.go tool
			// See testdata/download_test_page.go for details

			// Read golden file
			golden, err := os.ReadFile(goldenFile)
			if err != nil {
				t.Fatalf("Failed to read golden file (run with -update flag to create): %v", err)
			}

			// Compare content
			if textContent != string(golden) {
				t.Errorf("Processed HTML doesn't match golden file.\nExpected: %d characters\nGot: %d characters\n"+
					"(Run testdata/download_test_page.go to update golden files)", len(golden), len(textContent))
			}
		})
	}
}
