package agentid

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerator_GetOrCreate(t *testing.T) {
	// Use temp directory for testing
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "agent-id")

	g := NewGenerator(storagePath)

	// First call should generate new ID
	id1, err := g.GetOrCreate()
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Validate format
	if !strings.HasPrefix(id1, "agent_") {
		t.Errorf("Invalid ID format: %s", id1)
	}

	// Should be 39 characters total: "agent_" (6) + 32 hex chars
	if len(id1) != 38 {
		t.Errorf("Invalid ID length: got %d, want 38", len(id1))
	}

	// Second call should return same ID
	id2, err := g.GetOrCreate()
	if err != nil {
		t.Fatalf("Second GetOrCreate failed: %v", err)
	}

	if id1 != id2 {
		t.Errorf("ID changed across calls: %s != %s", id1, id2)
	}

	// Verify file was created
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Error("Agent ID file was not created")
	}
}

func TestGenerator_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "agent-id")

	// Generate ID with first generator
	g1 := NewGenerator(storagePath)
	id1, err := g1.GetOrCreate()
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Create new generator instance (simulating restart)
	g2 := NewGenerator(storagePath)
	id2, err := g2.GetOrCreate()
	if err != nil {
		t.Fatalf("GetOrCreate after restart failed: %v", err)
	}

	// Should load same ID from disk
	if id1 != id2 {
		t.Errorf("ID not persisted correctly: %s != %s", id1, id2)
	}
}

func TestGenerator_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "agent-id")

	// Write invalid content
	if err := os.WriteFile(storagePath, []byte("invalid_format"), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	g := NewGenerator(storagePath)
	id, err := g.GetOrCreate()

	// Should generate new ID and overwrite invalid file
	if err != nil {
		t.Fatalf("Expected new ID generation for invalid file, got error: %v", err)
	}

	if !strings.HasPrefix(id, "agent_") {
		t.Errorf("Generated ID has invalid format: %s", id)
	}
}

func TestGenerator_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "agent-id")

	// Write empty file
	if err := os.WriteFile(storagePath, []byte(""), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	g := NewGenerator(storagePath)
	id, err := g.GetOrCreate()
	if err != nil {
		t.Fatalf("Expected new ID generation for empty file, got error: %v", err)
	}

	if !strings.HasPrefix(id, "agent_") {
		t.Errorf("Generated ID has invalid format: %s", id)
	}
}

func TestGenerator_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	// Use nested path that doesn't exist
	storagePath := filepath.Join(tmpDir, "connektn", "data", "agent-id")

	g := NewGenerator(storagePath)
	_, err := g.GetOrCreate()
	if err != nil {
		t.Fatalf("GetOrCreate failed to create directory: %v", err)
	}

	// Verify directory was created
	dir := filepath.Dir(storagePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}
}

func TestGenerator_Uniqueness(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		subDir := filepath.Join(tmpDir, fmt.Sprintf("test-%d", i))
		storagePath := filepath.Join(subDir, "agent-id")
		g := NewGenerator(storagePath)
		id, err := g.GetOrCreate()
		if err != nil {
			t.Fatalf("GetOrCreate failed at iteration %d: %v", i, err)
		}

		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}

	if len(ids) != 100 {
		t.Errorf("Expected 100 unique IDs, got %d", len(ids))
	}
}
