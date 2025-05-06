// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package main

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

//go:embed testdata
var testdataFS embed.FS

var testFS fs.FS

func init() {
	t, err := fs.Sub(testdataFS, "testdata")
	if err != nil {
		panic(err)
	}
	testFS = t
}

func TestHandleCityData(t *testing.T) {
	s, err := newServer(t.Context(), nil, "testgpt", testFS)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Root path should list files",
			path:           "/city-data",
			expectedStatus: http.StatusOK,
			expectedBody:   "Data Files:",
		},
		{
			name:           "Root path with trailing slash should list files",
			path:           "/city-data/",
			expectedStatus: http.StatusOK,
			expectedBody:   "Data Files:",
		},
		{
			name:           "Existing file path should return file content",
			path:           "/city-data/test.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "Test file content",
		},
		{
			name:           "Non-existent file should return 404",
			path:           "/city-data/non_existent_file.txt",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "File not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			http.HandlerFunc(s.handleCityData).ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tc.expectedStatus)
			}

			if !strings.Contains(rr.Body.String(), tc.expectedBody) {
				t.Errorf("handler returned unexpected body: got %v, want it to contain %v", rr.Body.String(), tc.expectedBody)
			}
		})
	}
}

// TestTemplateHeaderTitle verifies that the templates are using the HeaderTitle field correctly
func TestTemplateHeaderTitle(t *testing.T) {
	s, err := newServer(t.Context(), nil, "TestApp", testFS)
	if err != nil {
		t.Fatal(err)
	}

	// Test the About page which we just fixed
	t.Run("About page should render with HeaderTitle", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/about", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.RemoteAddr = "127.0.0.1"
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(s.handleAbout)
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		want := "TestApp About"
		if got := rr.Body.String(); !strings.Contains(got, want) {
			t.Errorf("handler returned unexpected body: expected it to contain %q\n%q", want, got)
		}
	})
}
