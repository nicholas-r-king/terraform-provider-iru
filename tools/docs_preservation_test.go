// SPDX-License-Identifier: MPL-2.0
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedDocsPreserveUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "generated")
	dest := filepath.Join(root, "docs")
	for _, dir := range []string{source, dest} {
		if err := os.MkdirAll(filepath.Join(dir, "resources"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	extra := filepath.Join(dest, "resources", "blueprint 2.md")
	os.WriteFile(extra, []byte("untracked user content"), 0600)
	os.WriteFile(filepath.Join(source, "resources", "blueprint.md"), []byte("generated"), 0600)
	if err := copyGeneratedDocs(source, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(extra)
	if err != nil || string(got) != "untracked user content" {
		t.Fatal("untracked documentation was modified")
	}
	got, err = os.ReadFile(filepath.Join(dest, "resources", "blueprint.md"))
	if err != nil || string(got) != "generated" {
		t.Fatal("generated documentation absent")
	}
}
func TestGeneratedDocsRejectDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "generated")
	dest := filepath.Join(root, "docs")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{source, dest, outside} {
		os.MkdirAll(dir, 0755)
	}
	os.MkdirAll(filepath.Join(source, "resources"), 0755)
	os.WriteFile(filepath.Join(source, "resources", "blueprint.md"), []byte("generated"), 0600)
	if err := os.Symlink(outside, filepath.Join(dest, "resources")); err != nil {
		t.Skip(err)
	}
	if err := copyGeneratedDocs(source, dest); err == nil {
		t.Fatal("destination symlink accepted")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote outside docs")
	}
}
