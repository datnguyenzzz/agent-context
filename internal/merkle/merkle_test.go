package merkle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMerkleHashingAndDiffing(t *testing.T) {
	// Create a temporary directory for test workspace
	tmpDir, err := os.MkdirTemp("", "merkle-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectories and some indexable files
	err = os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)
	if err != nil {
		t.Fatalf("failed to create src dir: %v", err)
	}

	file1Path := filepath.Join(tmpDir, "main.go")
	file2Path := filepath.Join(tmpDir, "src", "helper.go")
	file3Path := filepath.Join(tmpDir, "unrelated.txt")

	err = os.WriteFile(file1Path, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}"), 0644)
	if err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	err = os.WriteFile(file2Path, []byte("package src\n\nfunc Help() {}"), 0644)
	if err != nil {
		t.Fatalf("failed to write helper.go: %v", err)
	}

	err = os.WriteFile(file3Path, []byte("some unrelated text file"), 0644)
	if err != nil {
		t.Fatalf("failed to write unrelated.txt: %v", err)
	}

	// 1. Build Merkle Tree for the initial state
	tree1, err := BuildMerkleTree(tmpDir, "")
	if err != nil {
		t.Fatalf("failed to build tree1: %v", err)
	}

	if tree1 == nil {
		t.Fatalf("tree1 should not be nil")
	}

	// Verify only Go files are present in the tree
	files := collectFiles(tree1)
	if len(files) != 2 {
		t.Errorf("expected 2 files in tree1, got %d: %v", len(files), files)
	}

	// Ensure main.go and src/helper.go are mapped, but not unrelated.txt
	hasMain := false
	hasHelper := false
	for _, f := range files {
		if f == "main.go" {
			hasMain = true
		}
		if f == "src/helper.go" {
			hasHelper = true
		}
	}
	if !hasMain || !hasHelper {
		t.Errorf("tree1 missing expected files. files: %v", files)
	}

	// 2. Diffing with same tree should return empty results
	added, modified, deleted := DiffTrees(tree1, tree1)
	if len(added) != 0 || len(modified) != 0 || len(deleted) != 0 {
		t.Errorf("diffing identical trees should yield no changes, got: added=%v, modified=%v, deleted=%v", added, modified, deleted)
	}

	// 3. Modifying a file
	err = os.WriteFile(file2Path, []byte("package src\n\nfunc Help() {\n\tprintln(\"modified\")\n}"), 0644)
	if err != nil {
		t.Fatalf("failed to update helper.go: %v", err)
	}

	tree2, err := BuildMerkleTree(tmpDir, "")
	if err != nil {
		t.Fatalf("failed to build tree2: %v", err)
	}

	added, modified, deleted = DiffTrees(tree1, tree2)
	if len(added) != 0 || len(modified) != 1 || len(deleted) != 0 || modified[0] != "src/helper.go" {
		t.Errorf("expected src/helper.go to be modified, got: added=%v, modified=%v, deleted=%v", added, modified, deleted)
	}

	// 4. Adding a file
	file4Path := filepath.Join(tmpDir, "src", "config.tf")
	err = os.WriteFile(file4Path, []byte(`resource "aws_s3_bucket" "test" {}`), 0644)
	if err != nil {
		t.Fatalf("failed to write config.tf: %v", err)
	}

	tree3, err := BuildMerkleTree(tmpDir, "")
	if err != nil {
		t.Fatalf("failed to build tree3: %v", err)
	}

	added, modified, deleted = DiffTrees(tree2, tree3)
	if len(added) != 1 || len(modified) != 0 || len(deleted) != 0 || added[0] != "src/config.tf" {
		t.Errorf("expected src/config.tf to be added, got: added=%v, modified=%v, deleted=%v", added, modified, deleted)
	}

	// 5. Deleting a file
	err = os.Remove(file1Path)
	if err != nil {
		t.Fatalf("failed to delete main.go: %v", err)
	}

	tree4, err := BuildMerkleTree(tmpDir, "")
	if err != nil {
		t.Fatalf("failed to build tree4: %v", err)
	}

	added, modified, deleted = DiffTrees(tree3, tree4)
	if len(added) != 0 || len(modified) != 0 || len(deleted) != 1 || deleted[0] != "main.go" {
		t.Errorf("expected main.go to be deleted, got: added=%v, modified=%v, deleted=%v", added, modified, deleted)
	}

	// 6. Adding a YAML file
	file5Path := filepath.Join(tmpDir, "config.yaml")
	err = os.WriteFile(file5Path, []byte("env: production\nport: 8080\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	tree5, err := BuildMerkleTree(tmpDir, "")
	if err != nil {
		t.Fatalf("failed to build tree5: %v", err)
	}

	added, modified, deleted = DiffTrees(tree4, tree5)
	if len(added) != 1 || len(modified) != 0 || len(deleted) != 0 || added[0] != "config.yaml" {
		t.Errorf("expected config.yaml to be added, got: added=%v, modified=%v, deleted=%v", added, modified, deleted)
	}
}
