package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPathHasHomebrew(t *testing.T) {
	if path := filepath.SplitList(os.Getenv("PATH")); !slices.Contains(path, "/opt/homebrew/bin") || !slices.Contains(path, "/usr/local/bin") {
		t.Fatalf("PATH = %v", path)
	}
}
