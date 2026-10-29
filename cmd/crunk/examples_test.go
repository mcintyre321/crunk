package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mcintyre321/crunk/internal/lang"
)

func TestExamplesAreValid(t *testing.T) {
	entries, err := os.ReadDir("../../examples")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			if _, diags := lang.Load(filepath.Join("../../examples", e.Name()), ".", lang.DefaultLimits()); diags.HasErrors() {
				t.Fatal(diags.Error())
			}
		})
	}
}
