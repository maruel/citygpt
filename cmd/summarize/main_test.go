// Copyright 2024 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile(t *testing.T) {
	// Create a temporary test file
	testContent := "This is a test file content."
	tempFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tempFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test reading the file
	content, err := readFile(tempFile)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	if content != testContent {
		t.Errorf("Expected content: %q, got: %q", testContent, content)
	}

	// Test reading a non-existent file
	_, err = readFile("non_existent_file.txt")
	if err == nil {
		t.Error("Expected an error when reading non-existent file, but got none")
	}
}
