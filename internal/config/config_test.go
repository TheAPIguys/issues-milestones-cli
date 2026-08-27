package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetLastRepositoryWritesReadableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	stored := Config{path: path}
	if err := stored.SetLastRepository("octo/demo"); err != nil {
		t.Fatalf("SetLastRepository() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) == "" || stored.LastRepository != "octo/demo" {
		t.Fatalf("stored config = %q, %#v", data, stored)
	}
}
