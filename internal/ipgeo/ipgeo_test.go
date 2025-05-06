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

func TestIPLookup(t *testing.T) {
	g, err := NewGeoIPChecker()
	if err != nil {
		t.Skip(err.Error())
	}

	tests := []struct {
		name     string
		ip       net.IP
		expected string
	}{
		{
			name:     "Canadian IP",
			ip:       net.IPv4(204, 48, 77, 92),
			expected: "CA",
		},
		{
			name:     "Tailscale IP - lower bound",
			ip:       net.IPv4(100, 64, 0, 1),
			expected: "local",
		},
		{
			name:     "Tailscale IP - middle range",
			ip:       net.IPv4(100, 100, 42, 42),
			expected: "local",
		},
		{
			name:     "Tailscale IP - upper bound",
			ip:       net.IPv4(100, 127, 255, 254),
			expected: "local",
		},
		{
			name:     "Private IP",
			ip:       net.IPv4(192, 168, 1, 1),
			expected: "local",
		},
		{
			name:     "Loopback IP",
			ip:       net.IPv4(127, 0, 0, 1),
			expected: "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := g.GetCountry(tt.ip)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.expected {
				t.Fatalf("wanted %s, got %s", tt.expected, got)
			}
		})
	}
}

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
