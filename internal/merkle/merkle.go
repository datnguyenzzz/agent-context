package merkle

// ponytail: Merkle tree-based incremental indexer to prune unchanged code subtrees, avoiding redundant chunking & LLM embedding generation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-mem/internal/db"
	"agent-mem/internal/llm"
	"agent-mem/internal/splitter"

	"github.com/google/uuid"
)

type NodeType string

const (
	NodeFile      NodeType = "file"
	NodeDirectory NodeType = "directory"
)

type MerkleNode struct {
	Type     NodeType               `json:"type"`
	Name     string                 `json:"name"`
	Path     string                 `json:"path"`
	Hash     string                 `json:"hash"`
	Children map[string]*MerkleNode `json:"children,omitempty"`
}

func sha256Hash(data string) string {
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func isIndexable(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	// ponytail: support Go, Terraform, YAML, and Markdown files
	return ext == ".go" || ext == ".tf" || ext == ".yaml" || ext == ".yml" || ext == ".md"
}

// BuildMerkleTree scans the filesystem recursively and constructs the Merkle Tree of indexable files
func BuildMerkleTree(absPath, relPath string) (*MerkleNode, error) {
	fullPath := filepath.Join(absPath, relPath)
	fi, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		if !isIndexable(fi.Name()) {
			return nil, nil
		}
		// Cap at 200KB for files to parse (consistent with main indexer)
		if fi.Size() > 200*1024 {
			return nil, nil
		}

		// Split file into AST-based chunks to determine the chunk structure & hashes
		chunks, err := splitter.SplitFile(fullPath)
		if err != nil {
			return nil, err
		}

		var chunkHashes []string
		for _, chunk := range chunks {
			chunkHash := sha256Hash(chunk.Content)
			chunkHashes = append(chunkHashes, chunkHash)
		}

		// Compute file hash from chunk hashes
		var fileHash string
		if len(chunkHashes) == 0 {
			fileHash = sha256Hash("")
		} else {
			fileHash = sha256Hash(strings.Join(chunkHashes, ""))
		}

		return &MerkleNode{
			Type: NodeFile,
			Name: fi.Name(),
			Path: relPath,
			Hash: fileHash,
		}, nil
	}

	// It's a directory, read its children
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	children := make(map[string]*MerkleNode)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			// Skip ignorable folders
			if name == ".git" || name == "node_modules" || name == "dist" || name == "bin" || name == "vendor" || name == ".gemini" {
				continue
			}
		}

		childRelPath := name
		if relPath != "" {
			childRelPath = filepath.Join(relPath, name)
		}

		childNode, err := BuildMerkleTree(absPath, childRelPath)
		if err != nil {
			return nil, err
		}
		if childNode != nil {
			children[name] = childNode
		}
	}

	// If directory contains no indexable files, exclude it from tree
	if len(children) == 0 {
		return nil, nil
	}

	// Calculate deterministic hash by sorting child nodes
	var keys []string
	for name := range children {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, name := range keys {
		sb.WriteString(name)
		sb.WriteString(children[name].Hash)
	}
	dirHash := sha256Hash(sb.String())

	return &MerkleNode{
		Type:     NodeDirectory,
		Name:     fi.Name(),
		Path:     relPath,
		Hash:     dirHash,
		Children: children,
	}, nil
}

// DiffTrees compares a previous and a new Merkle Tree to find added, modified, and deleted files.
// Prunes tree traversal instantly for matching node hashes.
func DiffTrees(prev, next *MerkleNode) (added, modified, deleted []string) {
	if prev == nil && next == nil {
		return
	}

	if prev == nil {
		// All files under next are added
		return collectFiles(next), nil, nil
	}

	if next == nil {
		// All files under prev are deleted
		return nil, nil, collectFiles(prev)
	}

	if prev.Hash == next.Hash {
		// Subtree is identical! Prune recursion.
		return nil, nil, nil
	}

	if prev.Type != next.Type {
		// Path changed from directory to file or vice-versa
		return collectFiles(next), nil, collectFiles(prev)
	}

	if prev.Type == NodeFile && next.Type == NodeFile {
		// Both are files, but hashes differ: modified
		return nil, []string{next.Path}, nil
	}

	// Recurse children
	for name, nextChild := range next.Children {
		prevChild := prev.Children[name]
		a, m, d := DiffTrees(prevChild, nextChild)
		added = append(added, a...)
		modified = append(modified, m...)
		deleted = append(deleted, d...)
	}

	for name, prevChild := range prev.Children {
		if _, exists := next.Children[name]; !exists {
			deleted = append(deleted, collectFiles(prevChild)...)
		}
	}

	return
}

func collectFiles(node *MerkleNode) []string {
	if node == nil {
		return nil
	}
	if node.Type == NodeFile {
		return []string{node.Path}
	}
	var files []string
	for _, child := range node.Children {
		files = append(files, collectFiles(child)...)
	}
	return files
}

// UpdateIndex implements the Merkle-tree based incremental indexing
func UpdateIndex(absPath string) (int, int, int, error) {
	if err := db.InitDatabase(); err != nil {
		return 0, 0, 0, fmt.Errorf("database init failed: %w", err)
	}

	// 1. Build the new Merkle tree from local codebase state
	newTree, err := BuildMerkleTree(absPath, "")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to build local Merkle tree: %w", err)
	}

	// If no indexable files found, clean up and exit
	if newTree == nil {
		// Retrieve previous tree to purge if it existed
		prevRoot, prevJSON, err := db.LoadMerkleTree(absPath)
		if err == nil && prevRoot != "" {
			var prevTree MerkleNode
			if json.Unmarshal([]byte(prevJSON), &prevTree) == nil {
				deleted := collectFiles(&prevTree)
				for _, path := range deleted {
					_ = db.DeleteFileMemories(absPath, path)
				}
			}
			_ = db.SaveMerkleTree(absPath, "", "{}")
		}
		return 0, 0, 0, nil
	}

	// 2. Load the previously stored Merkle tree
	prevRoot, prevJSON, err := db.LoadMerkleTree(absPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to load previous Merkle tree: %w", err)
	}

	var prevTree *MerkleNode
	if prevRoot != "" && prevJSON != "" && prevJSON != "{}" {
		var pt MerkleNode
		if err := json.Unmarshal([]byte(prevJSON), &pt); err == nil {
			prevTree = &pt
		}
	}

	// 3. Diff trees
	added, modified, deleted := DiffTrees(prevTree, newTree)

	// 4. Delete stale memories of removed files
	for _, relPath := range deleted {
		fmt.Printf("✗ Removing stale memories for deleted file: %s\n", relPath)
		if err := db.DeleteFileMemories(absPath, relPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to clear memories for deleted file %s: %v\n", relPath, err)
		}
	}

	// 5. Delete stale memories of modified files
	for _, relPath := range modified {
		fmt.Printf("⚙ Purging stale memories for modified file: %s\n", relPath)
		if err := db.DeleteFileMemories(absPath, relPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to clear memories for modified file %s: %v\n", relPath, err)
		}
	}

	// 6. Index added and modified files
	filesToProcess := append(added, modified...)
	for _, relPath := range filesToProcess {
		fullPath := filepath.Join(absPath, relPath)
		fmt.Printf("⚙ Parsing AST & generating embeddings for %s...\n", relPath)

		chunks, err := splitter.SplitFile(fullPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to split %s: %v\n", relPath, err)
			continue
		}

		for _, chunk := range chunks {
			formattedContent := fmt.Sprintf("File: %s (Lines: %d-%d)\nContent:\n%s", relPath, chunk.StartLine, chunk.EndLine, chunk.Content)

			// Generate embedding via LiteLLM
			embedding, err := llm.GetEmbedding(formattedContent)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to generate embedding for %s (lines %d-%d): %v\n", relPath, chunk.StartLine, chunk.EndLine, err)
				continue
			}

			id := uuid.New().String()
			if err := db.SaveMemory(id, formattedContent, "project", absPath, embedding); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to save chunk to memory store: %v\n", err)
				continue
			}
		}
		fmt.Printf("✓ Successfully indexed %s (%d AST chunks)\n", relPath, len(chunks))
	}

	// 7. Save updated Merkle Tree state
	newTreeBytes, err := json.Marshal(newTree)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to serialize Merkle Tree to JSON: %w", err)
	}

	if err := db.SaveMerkleTree(absPath, newTree.Hash, string(newTreeBytes)); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to save Merkle Tree state: %w", err)
	}

	// 8. Generate high-level Codebase Profile if any changes occurred or if it is the first index
	if len(added) > 0 || len(modified) > 0 || len(deleted) > 0 || prevRoot == "" {
		fmt.Printf("⚙ Generating high-level Codebase Profile for %s...\n", filepath.Base(absPath))
		
		allFiles := collectFiles(newTree)
		maxFilesToPrompt := 50
		if len(allFiles) > maxFilesToPrompt {
			allFiles = allFiles[:maxFilesToPrompt]
		}
		
		filesList := strings.Join(allFiles, "\n")
		prompt := fmt.Sprintf(`
You are a Software Architect analyzing a local codebase located at "%s".
Below is a subset of the indexable files in this repository:

"""
%s
"""

Generate a highly concise, authoritative "Codebase Profile" summary of this repository.
It MUST start exactly with:
[Codebase Profile] Codebase: %s (Path: %s)

Then provide:
1. A 1-sentence executive summary of the project's likely purpose.
2. Major languages and tools used.
3. High-level map of important folders/packages and their roles.

Keep the entire response under 10-12 lines of clean, bulleted text. Do not add markdown blocks outside the list.
`, absPath, filesList, filepath.Base(absPath), absPath)

		profile, err := llm.GenerateText(prompt)
		if err == nil && profile != "" {
			profileEmbedding, err := llm.GetEmbedding(profile)
			if err == nil {
				if err := db.SaveCodebaseProfile(absPath, profile, profileEmbedding); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to save codebase profile: %v\n", err)
				} else {
					fmt.Println("✓ Regenerated and saved Codebase Profile!")
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "Warning: Failed to generate codebase profile via LLM: %v\n", err)
		}
	}

	return len(added), len(modified), len(deleted), nil
}
