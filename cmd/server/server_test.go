package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/datnguyenzzz/semantic-grep/internal/callgraph"
	"github.com/datnguyenzzz/semantic-grep/internal/db"
	"github.com/datnguyenzzz/semantic-grep/internal/llm"
	mockllm "github.com/datnguyenzzz/semantic-grep/internal/llm/mocks"
	"github.com/datnguyenzzz/semantic-grep/internal/merkle"
	"github.com/datnguyenzzz/semantic-grep/internal/turboquant"
	"github.com/stretchr/testify/mock"
)

func Test_PeriodicIndexUpdate(t *testing.T) {
	// Set target dimension to 16 for test execution
	originalDim := turboquant.DefaultDimension
	turboquant.DefaultDimension = 16
	defer func() {
		turboquant.DefaultDimension = originalDim
	}()

	// 1. Set up a temporary home directory so we use a clean test database
	tmpHome, err := os.MkdirTemp("", "mcp-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp home: %v", err)
	}
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer func() {
		os.Setenv("HOME", originalHome)
		os.RemoveAll(tmpHome)
	}()

	// 2. Initialize the test database
	if err := db.InitDatabase(); err != nil {
		t.Fatalf("failed to init database: %v", err)
	}

	// 3. Create a temporary workspace to scan
	tmpWorkspace, err := os.MkdirTemp("", "workspace-*")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	tmpWorkspace, _ = filepath.EvalSymlinks(tmpWorkspace)
	defer os.RemoveAll(tmpWorkspace)

	// Create a dummy Go file inside the workspace so it is indexable
	dummyFile := filepath.Join(tmpWorkspace, "main.go")
	_ = os.WriteFile(dummyFile, []byte("package main\n\nfunc main() {\n\tprintln(\"Hello test!\")\n}"), 0644)

	// 4. Save codebase CWD in database
	if err := db.SaveMerkleTree(tmpWorkspace, "initial_hash", "{}"); err != nil {
		t.Fatalf("failed to save codebase: %v", err)
	}

	// Mock the LLM embedding client using the generated MockILLM
	mockLLM := mockllm.NewMockILLM(t)
	mockLLM.On("GetEmbedding", mock.Anything, mock.Anything).Return(func(text string, dim int) []float32 {
		mockVec := make([]float32, dim)
		mockVec[0] = 1.0
		return mockVec
	}, nil)

	llm.DefaultClient = mockLLM
	defer func() {
		llm.DefaultClient = &llm.LiteLLM{}
	}()

	// 5. Initialize TurboQuant and a test index
	tq, err := turboquant.NewTurboQuant(16, 4, 42) // small dimension 16 for faster execution
	if err != nil {
		t.Fatalf("failed to init TurboQuant: %v", err)
	}

	tqvPath := filepath.Join(tmpHome, "agent-mem.tqv")
	index, err := turboquant.NewIndex(tqvPath, tq)
	if err != nil {
		t.Fatalf("failed to init Index: %v", err)
	}

	// 6. Set short interval environment variable so background updates trigger quickly
	os.Setenv("BACKGROUND_SYNC_INTERVAL", "15ms")
	defer os.Unsetenv("BACKGROUND_SYNC_INTERVAL")

	// Disable verbose logs during test to keep output clean
	log.SetOutput(os.NewFile(0, os.DevNull))

	// 7. Start periodic background updates
	startPeriodicIndexUpdate(index)

	// Wait for 50 milliseconds to allow startup sweep and at least one periodic sweep to occur
	time.Sleep(50 * time.Millisecond)

	// Verify that the codebase main.go was successfully scanned and indexed into DuckDB gemini_memories!
	memories, err := db.SearchMemories("Hello test!", make([]float32, 16), tmpWorkspace, 5, index)
	if err != nil {
		t.Fatalf("failed to search memories: %v", err)
	}

	foundMain := false
	for _, m := range memories {
		if m.ID != "" && m.CWD == filepath.Join(tmpWorkspace, "main.go") {
			foundMain = true
			break
		}
	}

	if !foundMain {
		t.Errorf("expected background periodic update to successfully index main.go from workspace %s", tmpWorkspace)
	}
}

func Test_PopulateCallGraphContentConcurrent(t *testing.T) {
	// Set target dimension to 16 for test execution
	originalDim := turboquant.DefaultDimension
	turboquant.DefaultDimension = 16
	defer func() {
		turboquant.DefaultDimension = originalDim
	}()

	tmpDir, err := os.MkdirTemp("", "mcp-concurrent-cg-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	if err := db.InitDatabase(); err != nil {
		t.Fatalf("failed to init database: %v", err)
	}

	// Setup mock TQ index
	tq, err := turboquant.NewTurboQuant(16, 4, 42)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	tqvPath := filepath.Join(tmpDir, "test_fts_conj.tqv")
	index, err := turboquant.NewIndex(tqvPath, tq)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	// 1. Write different files to represent a realistic, complex call graph with structs & receiver methods

	// File A: models.go
	modelsPath := filepath.Join(tmpDir, "models.go")
	modelsContent := `package main

type DB struct {
	URI string
}

func (db *DB) Connect() error {
	println("connecting to URI")
	return nil
}
`
	if err := os.WriteFile(modelsPath, []byte(modelsContent), 0644); err != nil {
		t.Fatalf("failed to write models.go: %v", err)
	}

	// File B: service.go
	servicePath := filepath.Join(tmpDir, "service.go")
	serviceContent := `package main

type Service struct {
	Database *DB
}

func (s *Service) Process(data string) error {
	s.Database.Connect()
	println("processing data:", data)
	return nil
}
`
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		t.Fatalf("failed to write service.go: %v", err)
	}

	// File C: main.go
	mainPath := filepath.Join(tmpDir, "main.go")
	mainContent := `package main

func main() {
	db := &DB{URI: "localhost"}
	svc := &Service{Database: db}
	svc.Process("test-data")
}
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	// 2. Mock the LiteLLM Client
	mockLLM := mockllm.NewMockILLM(t)
	mockLLM.On("GetEmbedding", mock.Anything, mock.Anything).Return(func(text string, dim int) []float32 {
		mockVec := make([]float32, dim)
		mockVec[0] = 1.0
		return mockVec
	}, nil)

	originalClient := llm.DefaultClient
	llm.DefaultClient = mockLLM
	defer func() { llm.DefaultClient = originalClient }()

	// 3. Save codebase CWD in database and build index
	if err := db.SaveMerkleTree(tmpDir, "initial_hash", "{}"); err != nil {
		t.Fatalf("failed to save codebase: %v", err)
	}

	_, _, _, err = merkle.UpdateIndex(tmpDir, index)
	if err != nil {
		t.Fatalf("failed to run UpdateIndex: %v", err)
	}

	// 4. Retrieve Service.Process node directly from DB
	targetNode, err := db.GetCallNode("(*Service).Process")
	if err != nil || targetNode == nil {
		t.Fatalf("failed to retrieve target node: %v", err)
	}

	// 5. Generate on-demand bi-directional execution tree
	report, err := callgraph.GenerateOnDemandTreeReport(targetNode, "both", 2, db.GetCallees, db.GetCallers)
	if err != nil {
		t.Fatalf("failed to generate tree report: %v", err)
	}

	// Verify our graph has the correct nodes
	if len(report.Callers) != 1 || report.Callers[0].SymbolName != "main" {
		t.Fatalf("unexpected callers structure in E2E call graph: %v", report.Callers)
	}
	if len(report.Callees) != 1 || report.Callees[0].SymbolName != "(*DB).Connect" {
		t.Fatalf("unexpected callees structure in E2E call graph: %v", report.Callees)
	}

	// 6. Assertions on all 3 node ranges and file paths
	if report.TargetNode.StartLine != 7 || report.TargetNode.EndLine != 11 || !strings.HasSuffix(report.TargetNode.FilePath, "service.go") {
		t.Errorf("unexpected target node metadata: %+v", report.TargetNode)
	}
	if report.Callers[0].StartLine != 3 || report.Callers[0].EndLine != 7 || !strings.HasSuffix(report.Callers[0].FilePath, "main.go") {
		t.Errorf("unexpected caller node metadata: %+v", report.Callers[0])
	}
	if report.Callees[0].StartLine != 7 || report.Callees[0].EndLine != 10 || !strings.HasSuffix(report.Callees[0].FilePath, "models.go") {
		t.Errorf("unexpected callee node metadata: %+v", report.Callees[0])
	}
}

func Test_PopulateCallGraphContent_Python(t *testing.T) {
	// End-to-end Python: index a multi-file Python codebase through the real merkle pipeline,
	// then verify both the persisted call graph (callers/callees) and that a Python chunk is searchable.
	originalDim := turboquant.DefaultDimension
	turboquant.DefaultDimension = 16
	defer func() { turboquant.DefaultDimension = originalDim }()

	tmpDir, err := os.MkdirTemp("", "mcp-cg-e2e-py-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	if err := db.InitDatabase(); err != nil {
		t.Fatalf("failed to init database: %v", err)
	}

	tq, err := turboquant.NewTurboQuant(16, 4, 42)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	tqvPath := filepath.Join(tmpDir, "test_py_cg.tqv")
	index, err := turboquant.NewIndex(tqvPath, tq)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	// 1. Module-level Python functions across three files: main -> process -> connect
	utilPath := filepath.Join(tmpDir, "util.py")
	utilContent := `def connect():
    print("connecting")
    return None
`
	if err := os.WriteFile(utilPath, []byte(utilContent), 0644); err != nil {
		t.Fatalf("failed to write util.py: %v", err)
	}

	servicePath := filepath.Join(tmpDir, "service.py")
	serviceContent := `def process(data):
    connect()
    print("processing")
    return None
`
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		t.Fatalf("failed to write service.py: %v", err)
	}

	mainPath := filepath.Join(tmpDir, "main.py")
	mainContent := `def main():
    process("test-data")
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.py: %v", err)
	}

	// 2. Mock the LiteLLM client
	mockLLM := mockllm.NewMockILLM(t)
	mockLLM.On("GetEmbedding", mock.Anything, mock.Anything).Return(func(text string, dim int) []float32 {
		mockVec := make([]float32, dim)
		mockVec[0] = 1.0
		return mockVec
	}, nil)
	originalClient := llm.DefaultClient
	llm.DefaultClient = mockLLM
	defer func() { llm.DefaultClient = originalClient }()

	// 3. Register codebase and run the real indexing pipeline
	if err := db.SaveMerkleTree(tmpDir, "initial_hash", "{}"); err != nil {
		t.Fatalf("failed to save codebase: %v", err)
	}
	if _, _, _, err = merkle.UpdateIndex(tmpDir, index); err != nil {
		t.Fatalf("failed to run UpdateIndex: %v", err)
	}

	// 4. Retrieve the Python module function node directly from DB
	targetNode, err := db.GetCallNode("process")
	if err != nil || targetNode == nil {
		t.Fatalf("failed to retrieve target node: %v", err)
	}

	// 5. Bi-directional execution tree: main -> process -> connect
	report, err := callgraph.GenerateOnDemandTreeReport(targetNode, "both", 2, db.GetCallees, db.GetCallers)
	if err != nil {
		t.Fatalf("failed to generate tree report: %v", err)
	}

	if len(report.Callers) != 1 || report.Callers[0].SymbolName != "main" {
		t.Fatalf("unexpected callers structure in Python E2E call graph: %v", report.Callers)
	}
	if len(report.Callees) != 1 || report.Callees[0].SymbolName != "connect" {
		t.Fatalf("unexpected callees structure in Python E2E call graph: %v", report.Callees)
	}

	// 6. Node metadata / file paths
	if report.TargetNode.StartLine != 1 || report.TargetNode.EndLine != 4 || !strings.HasSuffix(report.TargetNode.FilePath, "service.py") {
		t.Errorf("unexpected target node metadata: %+v", report.TargetNode)
	}
	if report.Callers[0].StartLine != 1 || report.Callers[0].EndLine != 2 || !strings.HasSuffix(report.Callers[0].FilePath, "main.py") {
		t.Errorf("unexpected caller node metadata: %+v", report.Callers[0])
	}
	if report.Callees[0].StartLine != 1 || report.Callees[0].EndLine != 3 || !strings.HasSuffix(report.Callees[0].FilePath, "util.py") {
		t.Errorf("unexpected callee node metadata: %+v", report.Callees[0])
	}

	// 7. The Python source must also be indexed as a searchable memory chunk
	memories, err := db.SearchMemories("process the data", make([]float32, 16), tmpDir, 5, index)
	if err != nil {
		t.Fatalf("failed to search memories: %v", err)
	}
	foundPy := false
	for _, m := range memories {
		if strings.HasSuffix(m.CWD, ".py") {
			foundPy = true
			break
		}
	}
	if !foundPy {
		t.Errorf("expected at least one indexed Python (.py) memory chunk, got: %v", memories)
	}
}
