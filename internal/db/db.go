package db

// ponytail: keep db operations simple, open and close on every call, and compress all embeddings dynamically using 4-bit TurboQuant for 12x space savings

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-mem/internal/turboquant"

	_ "github.com/duckdb/duckdb-go/v2"
)

type Memory struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	Category   string    `json:"category"`
	CWD        string    `json:"cwd"`
	Similarity float64   `json:"similarity,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func getDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dbDir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dbDir, "agent-mem.db"), nil
}

func Open() (*sql.DB, error) {
	dbPath, err := getDBPath()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func InitDatabase() error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()

	// ponytail: automatic DuckDB schema migration - drop old FLOAT[] tables and recreate with BLOB for TurboQuant compression
	var colType string
	err = db.QueryRow("SELECT data_type FROM information_schema.columns WHERE table_name = 'gemini_memories' AND column_name = 'embedding'").Scan(&colType)
	if err == nil {
		if strings.Contains(strings.ToUpper(colType), "FLOAT") || strings.Contains(strings.ToUpper(colType), "ARRAY") {
			_, _ = db.Exec("DROP TABLE gemini_memories")
		}
	}

	// ponytail: store embeddings as BLOB for extremely efficient 4-bit vector quantization
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS gemini_memories (
			id VARCHAR PRIMARY KEY,
			content TEXT NOT NULL,
			category VARCHAR NOT NULL,
			cwd TEXT NOT NULL,
			embedding BLOB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// ponytail: create merkle_trees table for tracking indexed files and directories to enable ultra-fast incremental indexing
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS merkle_trees (
			cwd TEXT PRIMARY KEY,
			root_hash VARCHAR NOT NULL,
			tree_json TEXT NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func SaveMemory(id, content, category, cwd string, embedding []float32) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()

	// ponytail: quantize float32 embedding to 4-bit to compress storage from 6144 bytes to 768 bytes for 1536-dimensional vectors
	dim := len(embedding)
	tq, err := turboquant.NewTurboQuant(dim, 4, 42)
	if err != nil {
		return fmt.Errorf("failed to init turboquant for saving: %w", err)
	}

	qv, err := tq.Quantize(embedding)
	if err != nil {
		return fmt.Errorf("failed to quantize embedding: %w", err)
	}

	serializedBytes, err := tq.Serialize(qv)
	if err != nil {
		return fmt.Errorf("failed to serialize quantized vector: %w", err)
	}

	query := `
		INSERT OR REPLACE INTO gemini_memories (id, content, category, cwd, embedding)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err = db.Exec(query, id, content, category, cwd, serializedBytes)
	return err
}

func SearchMemories(queryEmbedding []float32, category, cwd string, limit int) ([]Memory, error) {
	db, err := Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// ponytail: retrieve quantized vector BLOBs, dequantize on the fly, and score using Go-level CosineSimilarity
	query := `
		SELECT id, content, category, cwd, created_at, embedding
		FROM gemini_memories
	`

	var conditions []string
	var args []any

	if category != "" {
		conditions = append(conditions, fmt.Sprintf("category = $%d", len(args)+1))
		args = append(args, category)
	}

	if cwd != "" {
		conditions = append(conditions, fmt.Sprintf("(cwd = $%d OR category = 'personal')", len(args)+1))
		args = append(args, cwd)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dim := len(queryEmbedding)
	tq, err := turboquant.NewTurboQuant(dim, 4, 42)
	if err != nil {
		return nil, fmt.Errorf("failed to init turboquant for search: %w", err)
	}

	var memories []Memory
	for rows.Next() {
		var m Memory
		var embeddingBytes []byte
		err := rows.Scan(&m.ID, &m.Content, &m.Category, &m.CWD, &m.CreatedAt, &embeddingBytes)
		if err != nil {
			return nil, err
		}

		if len(embeddingBytes) > 0 {
			qv, err := tq.Deserialize(embeddingBytes)
			if err != nil {
				continue
			}

			dequantized, err := tq.Dequantize(qv)
			if err != nil {
				continue
			}

			sim, err := turboquant.CosineSimilarity(queryEmbedding, dequantized)
			if err != nil {
				continue
			}
			m.Similarity = sim
		}

		memories = append(memories, m)
	}

	// Sort results by similarity descending
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Similarity > memories[j].Similarity
	})

	if len(memories) > limit {
		memories = memories[:limit]
	}

	return memories, nil
}

func GetRecentMemories(cwd string, limit int) ([]Memory, error) {
	db, err := Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `
		SELECT id, content, category, cwd, created_at
		FROM gemini_memories
		WHERE (category = 'project' AND cwd = $1) OR (category = 'personal')
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := db.Query(query, cwd, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		err := rows.Scan(&m.ID, &m.Content, &m.Category, &m.CWD, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}

	return memories, nil
}

// SaveMerkleTree stores the serialized Merkle Tree state for a codebase
func SaveMerkleTree(cwd, rootHash, treeJSON string) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()

	query := `
		INSERT OR REPLACE INTO merkle_trees (cwd, root_hash, tree_json, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
	`
	_, err = db.Exec(query, cwd, rootHash, treeJSON)
	return err
}

// LoadMerkleTree retrieves the previously saved Merkle Tree root hash and JSON for a codebase
func LoadMerkleTree(cwd string) (string, string, error) {
	db, err := Open()
	if err != nil {
		return "", "", err
	}
	defer db.Close()

	query := `
		SELECT root_hash, tree_json FROM merkle_trees WHERE cwd = $1
	`
	var rootHash, treeJSON string
	err = db.QueryRow(query, cwd).Scan(&rootHash, &treeJSON)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return rootHash, treeJSON, err
}

// DeleteFileMemories deletes the existing chunk memories of a specific codebase file
func DeleteFileMemories(cwd, relPath string) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()

	// ponytail: delete chunks belonging to file using the standard "File: <relPath> (Lines: %" prefix convention
	query := `
		DELETE FROM gemini_memories
		WHERE category = 'project'
		  AND cwd = $1
		  AND content LIKE 'File: ' || $2 || ' (Lines:%'
	`
	_, err = db.Exec(query, cwd, relPath)
	return err
}

type Codebase struct {
	CWD       string    `json:"cwd"`
	RootHash  string    `json:"root_hash"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListCodebases returns all indexed codebases ordered by modification time
func ListCodebases() ([]Codebase, error) {
	db, err := Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `
		SELECT cwd, root_hash, updated_at
		FROM merkle_trees
		ORDER BY updated_at DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codebases []Codebase
	for rows.Next() {
		var c Codebase
		err := rows.Scan(&c.CWD, &c.RootHash, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		codebases = append(codebases, c)
	}
	return codebases, nil
}

// SaveCodebaseProfile stores the high-level summary of an indexed codebase as global personal memory
func SaveCodebaseProfile(cwd, profile string, embedding []float32) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()

	// Delete previous profile if any
	deleteQuery := `
		DELETE FROM gemini_memories
		WHERE category = 'personal'
		  AND cwd = $1
		  AND content LIKE '[Codebase Profile] Codebase: %'
	`
	_, _ = db.Exec(deleteQuery, cwd)

	// Save new profile
	embeddingSql, err := serializeQuantizedEmbedding(embedding)
	if err != nil {
		return err
	}
	id := "profile-" + filepath.Base(cwd)
	insertQuery := `
		INSERT OR REPLACE INTO gemini_memories (id, content, category, cwd, embedding)
		VALUES ($1, $2, 'personal', $3, $4)
	`

	_, err = db.Exec(insertQuery, id, profile, cwd, embeddingSql)
	return err
}

func serializeQuantizedEmbedding(embedding []float32) ([]byte, error) {
	dim := len(embedding)
	tq, err := turboquant.NewTurboQuant(dim, 4, 42)
	if err != nil {
		return nil, err
	}
	qv, err := tq.Quantize(embedding)
	if err != nil {
		return nil, err
	}
	return tq.Serialize(qv)
}
