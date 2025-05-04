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
	t.Run("with lists and headers", func(t *testing.T) {
		// Test with a file that has lists and headers that should be converted to Markdown
		testFile, err := os.Open("testdata/test_lists_headers.html")
		if err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		defer testFile.Close()

		text, title, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Check that the title was extracted correctly
		if title != "Heading Level 1" {
			t.Errorf("Expected title to be 'Heading Level 1', got '%s'", title)
		}

		// Check headers conversion
		headerTests := []struct {
			level int
			text  string
		}{
			{1, "# Heading Level 1"},
			{2, "## Heading Level 2"},
			{3, "### Heading Level 3"},
			{4, "#### Heading Level 4"},
			{5, "##### Heading Level 5"},
			{6, "###### Heading Level 6"},
		}

		for _, ht := range headerTests {
			if !strings.Contains(text, ht.text) {
				t.Errorf("Expected h%d to be converted to '%s'", ht.level, ht.text)
			}
		}

		// Check unordered lists
		if !strings.Contains(text, "- Unordered list item 1") {
			t.Error("Expected unordered list item to be converted to '- item'")
		}

		// Check nested unordered lists
		if !strings.Contains(text, "  - Nested unordered item 1") {
			t.Error("Expected nested unordered list item to have proper indentation")
		}

		// Check ordered lists
		if !strings.Contains(text, "1. Ordered list item 1") {
			t.Error("Expected ordered list item to be converted to '1. item'")
		}

		// Check nested ordered lists
		if !strings.Contains(text, "  1. Nested ordered item 1") {
			t.Error("Expected nested ordered list item to have proper indentation")
		}
	})

	t.Run("with block-mainpagecontent", func(t *testing.T) {
		// Test with a file that has a block-mainpagecontent div
		testFile, err := os.Open("testdata/test_mainpagecontent.html")
		if err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		defer testFile.Close()

		text, title, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Check that the title was extracted correctly
		if title != "Page Header" {
			t.Errorf("Expected title to be 'Page Header', got '%s'", title)
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

		text, title, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Check that title is extracted
		if title == "" {
			t.Error("Expected to extract a title, but got empty string")
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

	t.Run("with table content", func(t *testing.T) {
		// Test with a file that has an HTML table that should be converted to Markdown
		testFile, err := os.Open("testdata/test_table.html")
		if err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		defer testFile.Close()

		text, title, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Log the title for debugging purposes
		t.Logf("Extracted title: '%s'", title)

		// Check that the table header is present in Markdown format
		if !strings.Contains(text, "| Header 1 | Header 2 | Header 3 |") {
			t.Error("Expected table header row in Markdown format")
		}

		// Check for the separator row in Markdown format
		if !strings.Contains(text, "| ------- | ------- | ------- |") {
			t.Error("Expected table separator row in Markdown format")
		}

		// Check for the first data row in Markdown format
		if !strings.Contains(text, "| Row 1, Cell 1 | Row 1, Cell 2 | Row 1, Cell 3 |") {
			t.Error("Expected first data row in Markdown format")
		}

		// Check that text after the table is preserved
		if !strings.Contains(text, "Text after the table.") {
			t.Error("Expected text after the table to be extracted")
		}
	})

	t.Run("with complex table content", func(t *testing.T) {
		// Test with a file that has more complex HTML tables
		testFile, err := os.Open("testdata/test_complex_table.html")
		if err != nil {
			t.Fatalf("Failed to open test file: %v", err)
		}
		defer testFile.Close()

		text, title, err := ExtractTextFromHTML(testFile)
		if err != nil {
			t.Fatalf("ExtractTextFromHTML failed: %v", err)
		}

		// Log the title for debugging purposes
		t.Logf("Extracted title: '%s'", title)

		// Check for the first table with headers
		if !strings.Contains(text, "| Name | Age | Occupation |") {
			t.Error("Expected table with headers in Markdown format")
		}

		// Check for a data row from the first table
		if !strings.Contains(text, "| John Doe | 28 | Developer |") {
			t.Error("Expected data row from first table in Markdown format")
		}

		// Check for the second table (without explicit headers)
		if !strings.Contains(text, "| Cell 1 | Cell 2 | Cell 3 |") {
			t.Error("Expected headerless table row in Markdown format")
		}

		// Verify both tables were processed
		occurrences := strings.Count(text, "| ------- | ------- | ------- |")
		if occurrences != 2 {
			t.Errorf("Expected two table separator rows, found %d", occurrences)
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
