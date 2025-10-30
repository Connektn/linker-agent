// Package stripewebhook provides a secure webhook receiver for Stripe events.
// It verifies webhook signatures, enforces IP allowlists, and processes events
// through the matcher pipeline while maintaining zero-ownership principles.
//
// Threat model: This package assumes an adversary may attempt to:
// - Send forged webhook events without valid signatures
// - Replay old webhook events (timestamp validation)
// - Send requests from unauthorized IP addresses
// - Extract PII from logs or error messages
//
// Security: All webhook payloads are verified before processing. Only event
// metadata (id, type, created) is extracted for idempotency. Canonical objects
// are fetched directly from Stripe API. No PII is logged.
package stripewebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// VerifySignature verifies a Stripe webhook signature using HMAC-SHA256.
// It validates both the timestamp (to prevent replay attacks) and the
// signature itself.
//
// The Stripe-Signature header format is:
//   t=<timestamp>,v1=<signature>[,v0=<old_signature>]
//
// Signature is computed as: HMAC-SHA256(signingSecret, timestamp.payload)
//
// Returns an error if:
// - Header is missing or malformed
// - Timestamp is too old (exceeds maxSkew)
// - Signature does not match
//
// Security: Timing-safe comparison is used to prevent timing attacks.
func VerifySignature(header string, payload []byte, signingSecret string, maxSkew time.Duration) error {
	if header == "" {
		return fmt.Errorf("missing Stripe-Signature header")
	}

	// Parse the header: t=timestamp,v1=signature
	parts := strings.Split(header, ",")
	var timestamp int64
	var signature string

	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "t":
			var err error
			timestamp, err = strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid timestamp in Stripe-Signature: %w", err)
			}
		case "v1":
			signature = kv[1]
		}
	}

	if timestamp == 0 {
		return fmt.Errorf("missing timestamp in Stripe-Signature")
	}

	if signature == "" {
		return fmt.Errorf("missing v1 signature in Stripe-Signature")
	}

	// Check timestamp skew to prevent replay attacks
	eventTime := time.Unix(timestamp, 0)
	skew := time.Since(eventTime)
	if skew < 0 {
		skew = -skew
	}
	if skew > maxSkew {
		return fmt.Errorf("timestamp outside acceptable skew window")
	}

	// Compute expected signature: HMAC-SHA256(secret, timestamp.payload)
	signedPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(signedPayload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Timing-safe comparison
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// VerifyIPAllowlist checks if the remote address is in the allowed IP ranges.
// Returns an error if:
// - remoteAddr cannot be parsed
// - remoteAddr is not in any of the allowed CIDR ranges
//
// If allowedRanges is empty, all IPs are allowed (no filtering).
//
// Note: This is best-effort security. IP-based filtering can be bypassed by
// sophisticated attackers. Signature verification is the primary security control.
func VerifyIPAllowlist(remoteAddr string, allowedRanges []string) error {
	// If no allowlist configured, allow all
	if len(allowedRanges) == 0 {
		return nil
	}

	// Extract IP from "IP:port" format
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Try parsing as IP without port
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", host)
	}

	// Check if IP is in any allowed range
	for _, cidr := range allowedRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// Should not happen if config validation passed
			continue
		}

		if network.Contains(ip) {
			return nil
		}
	}

	return fmt.Errorf("IP address not in allowlist")
}
