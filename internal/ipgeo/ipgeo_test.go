// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package ipgeo

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRealIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expectedIP string
		wantErr    bool
	}{
		{
			name:       "Remote address only",
			remoteAddr: "192.168.1.1:1234",
			headers:    nil,
			expectedIP: "192.168.1.1",
			wantErr:    false,
		},
		{
			name:       "X-Forwarded-For header",
			remoteAddr: "10.0.0.1:1234",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 192.168.1.1"},
			expectedIP: "203.0.113.1",
			wantErr:    false,
		},
		{
			name:       "X-Real-IP header",
			remoteAddr: "10.0.0.1:1234",
			headers:    map[string]string{"X-Real-IP": "203.0.113.1"},
			expectedIP: "203.0.113.1",
			wantErr:    false,
		},
		{
			name:       "Both headers, X-Forwarded-For takes precedence",
			remoteAddr: "10.0.0.1:1234",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "203.0.113.2"},
			expectedIP: "203.0.113.1",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip, err := GetRealIP(req)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetRealIP() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if ip.String() != tt.expectedIP {
					t.Errorf("GetRealIP() = %v, expected %v", ip.String(), tt.expectedIP)
				}
			}
		})
	}
}

func TestMockIPChecker(t *testing.T) {
	checker := NewMockIPChecker()

	// Add some country codes to the map
	checker.CountryCodes["99.99.99.99"] = "CA"
	checker.CountryCodes["88.88.88.88"] = "US"
	checker.CountryCodes["77.77.77.77"] = "FR"

	tests := []struct {
		name     string
		ipStr    string
		expected string
		wantErr  bool
	}{
		{
			name:     "Canadian IP",
			ipStr:    "99.99.99.99",
			expected: "CA",
			wantErr:  false,
		},
		{
			name:     "US IP",
			ipStr:    "88.88.88.88",
			expected: "US",
			wantErr:  false,
		},
		{
			name:     "French IP",
			ipStr:    "77.77.77.77",
			expected: "FR",
			wantErr:  false,
		},
		{
			name:     "Unknown IP defaults to XX",
			ipStr:    "66.66.66.66",
			expected: "XX",
			wantErr:  false,
		},
		{
			name:     "Local IP returns 'local'",
			ipStr:    "127.0.0.1",
			expected: "local",
			wantErr:  false,
		},
		{
			name:     "Private IP returns 'local'",
			ipStr:    "192.168.1.1",
			expected: "local",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.IsFromCanada(net.ParseIP(tt.ipStr))

			if (err != nil) != tt.wantErr {
				t.Errorf("IsFromCanada() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if result != tt.expected {
				t.Errorf("IsFromCanada(%s) = %v, expected %v", tt.ipStr, result, tt.expected)
			}
		})
	}
}
