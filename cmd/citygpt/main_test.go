// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

//go:embed testdata
var testFS embed.FS

func TestHandleCityData(t *testing.T) {
	s := server{
		cityData: testFS,
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
			name:           "Directory path should list directory contents",
			path:           "/city-data/testdata",
			expectedStatus: http.StatusOK,
			expectedBody:   "Contents of testdata:",
		},
		{
			name:           "Existing file path should return file content",
			path:           "/city-data/testdata/test.txt",
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

// TestAppNameFlag verifies that the app-name flag works correctly
func TestAppNameFlag(t *testing.T) {
	// Test with default app name
	s := server{
		appName: "CityGPT",
	}
	if s.appName != "CityGPT" {
		t.Errorf("Default app name should be CityGPT, got %s", s.appName)
	}

	// Test with custom app name
	s = server{
		appName: "CustomName",
	}
	if s.appName != "CustomName" {
		t.Errorf("Custom app name should be CustomName, got %s", s.appName)
	}
}
