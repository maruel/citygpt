// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package ipgeo

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"
)

// IPChecker is an interface for services that can check if an IP is from a specified country
type IPChecker interface {
	// IsFromCanada returns the ISO country code for the given IP address.
	// Returns "CA" for Canadian IPs, other ISO codes for non-Canadian IPs,
	// and "local" for local, private, or unspecified IPs.
	IsFromCanada(ip net.IP) (string, error)
}

// GeoIPChecker implements IPChecker using the MaxMind GeoIP database
type GeoIPChecker struct {
	reader *maxminddb.Reader
}

// NewGeoIPChecker creates a new GeoIPChecker using the database file from user's config directory
func NewGeoIPChecker() (*GeoIPChecker, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	dbPath := filepath.Join(homeDir, ".config", "citygpt", "ipinfo_lite.mmdb")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("GeoIP database not found at %s. Please download it by following the instructions in the internal/ipgeo/README.md file", dbPath)
	}
	reader, err := maxminddb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoIP database: %w", err)
	}
	return &GeoIPChecker{reader: reader}, nil
}

// Close closes the underlying GeoIP database reader
func (g *GeoIPChecker) Close() error {
	if g.reader != nil {
		return g.reader.Close()
	}
	return nil
}

// IsFromCanada returns the ISO country code for the given IP address.
// Returns "CA" for Canadian IPs, other ISO codes for non-Canadian IPs,
// and "local" for local, private, or unspecified IPs.
func (g *GeoIPChecker) IsFromCanada(ip net.IP) (string, error) {
	if g.reader == nil {
		return "", errors.New("geoip database not initialized")
	}
	// Skip for private/local IPs
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return "local", nil
	}

	// TODO: Sounds inefficient.
	addr, err := netip.ParseAddr(ip.String())
	if err != nil {
		return "", err
	}
	var data struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err = g.reader.Lookup(addr).Decode(&data); err != nil {
		return "", err
	}
	return data.Country.ISOCode, nil
}

// MockIPChecker is a simple implementation of IPChecker for testing
type MockIPChecker struct {
	CountryCodes map[string]string
}

// NewMockIPChecker creates a new MockIPChecker
func NewMockIPChecker() *MockIPChecker {
	return &MockIPChecker{
		CountryCodes: make(map[string]string),
	}
}

// IsFromCanada returns the ISO country code for the given IP address
func (m *MockIPChecker) IsFromCanada(ip net.IP) (string, error) {
	if ip == nil {
		return "", errors.New("nil IP address")
	}

	// Skip for private/local IPs
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return "local", nil
	}

	ipStr := ip.String()
	countryCode, exists := m.CountryCodes[ipStr]
	if !exists {
		// If not explicitly set, default to not Canadian
		return "XX", nil
	}

	return countryCode, nil
}

// GetRealIP extracts the client's real IP address from an HTTP request,
// taking into account X-Forwarded-For or other proxy headers.
func GetRealIP(r *http.Request) (net.IP, error) {
	// Check X-Forwarded-For header (most common proxy header)
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		// X-Forwarded-For can contain multiple IPs, the client's IP is the first one
		ips := strings.Split(xForwardedFor, ",")
		ipStr := strings.TrimSpace(ips[0])
		ip := net.ParseIP(ipStr)
		if ip != nil {
			return ip, nil
		}
	}

	// Check X-Real-IP header (used by some proxies)
	xRealIP := r.Header.Get("X-Real-IP")
	if xRealIP != "" {
		ip := net.ParseIP(xRealIP)
		if ip != nil {
			return ip, nil
		}
	}

	// If no proxy headers found, get the remote address
	remoteAddr := r.RemoteAddr
	if remoteAddr != "" {
		// RemoteAddr might be in the format IP:port
		host, _, err := net.SplitHostPort(remoteAddr)
		if err == nil {
			ip := net.ParseIP(host)
			if ip != nil {
				return ip, nil
			}
		} else {
			// If SplitHostPort fails, try parsing the whole RemoteAddr as an IP
			ip := net.ParseIP(remoteAddr)
			if ip != nil {
				return ip, nil
			}
		}
	}

	return nil, errors.New("could not determine client IP address")
}
