// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// This tool downloads an HTML page from Ottawa's website and saves it unprocessed
// to generate a test case.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func mainImpl() error {
	const targetURL = "https://ottawa.ca/en/living-ottawa/laws-licences-and-permits/laws/laws-z/atv-orv-and-snowmobile-law-no-2019-421"
	fmt.Printf("Downloading HTML content from %s\n", targetURL)
	resp, err := http.Get(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	filename := filepath.Base(targetURL) + ".html"
	if err = os.WriteFile(filepath.Join("testdata", filename), content, 0o644); err != nil {
		return err
	}
	fmt.Printf("Successfully saved unprocessed HTML to %s\n", filename)
	fmt.Printf("This file can be used as test data for ottawa-data\n")
	fmt.Printf("\nReminder: Don't forget to add the new file to git:\n")
	fmt.Printf("git add %s\n", filename)
	return nil
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "download_test_page: %v\n", err)
		os.Exit(1)
	}
}
