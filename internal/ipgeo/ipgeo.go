// Copyright 2025 Marc-Antoine Ruel and Félix Lachapelle. All rights reserved.
// Use of this source code is governed under the AGPL v3
// that can be found in the LICENSE file.

package ipgeo

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/maruel/citygpt/internal"
	"github.com/oschwald/maxminddb-golang/v2"
)

// IPChecker is an interface for services that can check the country of an IP address
type IPChecker interface {
	io.Closer
	// GetCountry returns the ISO country code for the given IP address.
	// Returns "CA" for Canadian IPs, other ISO codes for non-Canadian IPs,
	// and "local" for local, private, or unspecified IPs.
	GetCountry(ip net.IP) (string, error)
}

// GeoIPChecker implements IPChecker using the MaxMind GeoIP database
type GeoIPChecker struct {
	reader *maxminddb.Reader
}

// NewGeoIPChecker creates a new GeoIPChecker using the database file from user's config directory
func NewGeoIPChecker() (*GeoIPChecker, error) {
	configDir, err := internal.GetConfigDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(configDir, "citygpt", "ipinfo_lite.mmdb")
	if _, err = os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("GeoIP database not found at %s. Please download it by following the instructions in the internal/ipgeo/README.md file", dbPath)
	}
	reader, err := maxminddb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoIP database: %w", err)
	}
	slog.Info("ipgeo", "loaded", dbPath)
	return &GeoIPChecker{reader: reader}, nil
}

// Close closes the underlying GeoIP database reader
func (g *GeoIPChecker) Close() error {
	return g.reader.Close()
}

// GetCountry returns the ISO country code for the given IP address.
// Returns "CA" for Canadian IPs, other ISO codes for non-Canadian IPs,
// and "local" for local, private, unspecified, or Tailscale IPs.
func (g *GeoIPChecker) GetCountry(ip net.IP) (string, error) {
	// Skip for private/local IPs
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return "local", nil
	}

	// Check for Tailscale IP (100.64.0.0/10 CGNAT range used by Tailscale)
	if ip4 := ip.To4(); ip4 != nil {
		// Tailscale uses 100.64.0.0/10 which is 100.64.0.0 through 100.127.255.255
		if int(ip4[0]) == 100 && int(ip4[1]) >= 64 && int(ip4[1]) <= 127 {
			return "local", nil
		}
	}

	// TODO: Sounds inefficient.
	addr, err := netip.ParseAddr(ip.String())
	if err != nil {
		return "", err
	}
	m := map[string]string{}
	if err = g.reader.Lookup(addr).DecodePath(&m); err != nil {
		return "", err
	}
	return m["country_code"], nil
}

// GetRealIP extracts the client's real IP address from an HTTP request,
// taking into account X-Forwarded-For or other proxy headers.
func GetRealIP(r *http.Request) (net.IP, error) {
	// Check X-Forwarded-For header (most common proxy header)
	if xForwardedFor := r.Header.Get("X-Forwarded-For"); xForwardedFor != "" {
		// X-Forwarded-For can contain multiple IPs, the client's IP is the first one
		ip := net.ParseIP(strings.TrimSpace(strings.Split(xForwardedFor, ",")[0]))
		if ip != nil {
			return ip, nil
		}
	}

	// Check X-Real-IP header (used by some proxies)
	if xRealIP := r.Header.Get("X-Real-IP"); xRealIP != "" {
		if ip := net.ParseIP(xRealIP); ip != nil {
			return ip, nil
		}
	}

	// If no proxy headers found, get the remote address
	if remoteAddr := r.RemoteAddr; remoteAddr != "" {
		// RemoteAddr might be in the format IP:port
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			if ip := net.ParseIP(host); ip != nil {
				return ip, nil
			}
		} else {
			// If SplitHostPort fails, try parsing the whole RemoteAddr as an IP
			if ip := net.ParseIP(remoteAddr); ip != nil {
				return ip, nil
			}
		}
	}
	return nil, errors.New("could not determine client IP address")
}
