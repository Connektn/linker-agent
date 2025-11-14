package agentid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultStoragePath is the default location for persisting the agent ID.
// Uses user's home directory to avoid requiring sudo permissions.
// In production Kubernetes deployments, this would be mounted as a persistent volume.
const DefaultStoragePath = "~/.connektn/agent-id"

// Generator handles agent ID generation and persistence
type Generator struct {
	storagePath string
}

// NewGenerator creates a new agent ID generator
func NewGenerator(storagePath string) *Generator {
	if storagePath == "" {
		storagePath = DefaultStoragePath
	}

	// Expand ~ to home directory
	storagePath = expandHomeDir(storagePath)

	return &Generator{
		storagePath: storagePath,
	}
}

// expandHomeDir expands ~ in path to the user's home directory
func expandHomeDir(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fall back to original path if we can't get home dir
		return path
	}

	if path == "~" {
		return homeDir
	}

	// Replace ~/... with /home/user/...
	return filepath.Join(homeDir, path[2:])
}

// GetOrCreate retrieves the existing agent ID from disk, or generates and persists a new one
//
// Threat model:
// - Agent ID is not a secret but must be unique and stable across restarts
// - Using crypto/rand ensures collision resistance
// - Persistence ensures identity continuity for tenant dashboard tracking
func (g *Generator) GetOrCreate() (string, error) {
	// Try to read existing ID
	if id, err := g.read(); err == nil {
		return id, nil
	}

	// Generate new ID
	id, err := g.generate()
	if err != nil {
		return "", fmt.Errorf("failed to generate agent ID: %w", err)
	}

	// Persist to disk
	if err := g.write(id); err != nil {
		return "", fmt.Errorf("failed to persist agent ID: %w", err)
	}

	return id, nil
}

// generate creates a new agent ID in the format "agent_{32-char-hex}"
func (g *Generator) generate() (string, error) {
	// Generate 16 random bytes (128 bits) for sufficient uniqueness
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto random generation failed: %w", err)
	}

	// Encode as hex and prefix with "agent_"
	return fmt.Sprintf("agent_%s", hex.EncodeToString(b)), nil
}

// read loads the agent ID from disk
func (g *Generator) read() (string, error) {
	data, err := os.ReadFile(g.storagePath)
	if err != nil {
		return "", err
	}

	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", fmt.Errorf("agent ID file is empty")
	}

	// Validate format
	if !strings.HasPrefix(id, "agent_") {
		return "", fmt.Errorf("invalid agent ID format: %s", id)
	}

	return id, nil
}

// write persists the agent ID to disk
func (g *Generator) write(id string) error {
	// Ensure directory exists
	dir := filepath.Dir(g.storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write with restrictive permissions (only owner can read/write)
	if err := os.WriteFile(g.storagePath, []byte(id), 0600); err != nil {
		return fmt.Errorf("failed to write agent ID file: %w", err)
	}

	return nil
}

// GetOrCreateDefault is a convenience function using the default storage path
func GetOrCreateDefault() (string, error) {
	g := NewGenerator("")
	return g.GetOrCreate()
}
