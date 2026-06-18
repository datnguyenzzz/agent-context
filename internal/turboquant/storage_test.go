package turboquant

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestStorage_LoadSave(t *testing.T) {
	// Create a temp directory for our storage file
	tmpDir, err := os.MkdirTemp("", "turboquant-storage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test_storage.tqlm")

	dim := 128
	bw := 4
	tq, err := NewTurboQuant(dim, bw, 42)
	if err != nil {
		t.Fatalf("failed to initialize TurboQuant: %v", err)
	}

	storage := NewStorage(dim, bw)

	// Create some mock quantized vectors and serialize them
	v1 := make([]float32, dim)
	v1[0] = 1.0
	qv1, _ := tq.Quantize(v1)
	ser1, _ := tq.Serialize(qv1)

	v2 := make([]float32, dim)
	v2[0] = -1.0
	qv2, _ := tq.Quantize(v2)
	ser2, _ := tq.Serialize(qv2)

	vectors := map[string][]byte{
		"id-1": ser1,
		"id-2": ser2,
	}

	// 1. Test Save
	err = storage.Save(filePath, tq, vectors)
	if err != nil {
		t.Fatalf("failed to save vectors: %v", err)
	}

	// 2. Test Load
	loaded, err := storage.Load(filePath, tq)
	if err != nil {
		t.Fatalf("failed to load vectors: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded vectors, got %d", len(loaded))
	}

	if _, ok := loaded["id-1"]; !ok {
		t.Errorf("expected id-1 to be loaded")
	}

	if _, ok := loaded["id-2"]; !ok {
		t.Errorf("expected id-2 to be loaded")
	}

	// Verify loaded vector fields match original exactly
	for id, originalSer := range vectors {
		loadedSer, ok := loaded[id]
		if !ok {
			t.Fatalf("expected vector %s to be loaded", id)
		}

		originalQv, err := tq.Deserialize(originalSer)
		if err != nil {
			t.Fatalf("failed to deserialize original vector: %v", err)
		}

		loadedQv, err := tq.Deserialize(loadedSer)
		if err != nil {
			t.Fatalf("failed to deserialize loaded vector: %v", err)
		}

		// 1. Verify Norm is exactly equal
		if loadedQv.Norm != originalQv.Norm {
			t.Errorf("vector %s norm mismatch: expected %f, got %f", id, originalQv.Norm, loadedQv.Norm)
		}

		// 2. Verify Indices length matches exactly
		if len(loadedQv.Indices) != len(originalQv.Indices) {
			t.Errorf("vector %s indices length mismatch: expected %d, got %d", id, len(originalQv.Indices), len(loadedQv.Indices))
			continue
		}

		// 3. Verify each coordinate/index is exactly equal
		for i := 0; i < len(originalQv.Indices); i++ {
			if loadedQv.Indices[i] != originalQv.Indices[i] {
				t.Errorf("vector %s index at offset %d mismatch: expected %d, got %d", id, i, originalQv.Indices[i], loadedQv.Indices[i])
				break
			}
		}
	}
}

func TestStorage_BackwardCompatibility(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "turboquant-storage-compat-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "v1_storage.tqlm")

	dim := 16
	bw := 4
	tq, err := NewTurboQuant(dim, bw, 42)
	if err != nil {
		t.Fatalf("failed to initialize TurboQuant: %v", err)
	}

	storage := NewStorage(dim, bw)

	// Create raw version 1 file manually
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// 16-byte uncompressed header
	header := make([]byte, 16)
	copy(header[0:4], []byte("TQLM"))
	binary.LittleEndian.PutUint32(header[4:8], 1) // version 1 (uncompressed)
	binary.LittleEndian.PutUint32(header[8:12], 1) // 1 vector
	binary.LittleEndian.PutUint32(header[12:16], uint32(storage.bytesPerVector))
	f.Write(header)

	// 5-byte metadata
	meta := make([]byte, 5)
	binary.LittleEndian.PutUint32(meta[0:4], uint32(dim))
	meta[4] = uint8(bw)
	f.Write(meta)

	// Vector record: 36 bytes id + serialized bytes
	v1 := make([]float32, dim)
	v1[0] = 1.0
	qv1, _ := tq.Quantize(v1)
	ser1, _ := tq.Serialize(qv1)

	recordBuf := make([]byte, storage.bytesPerVector)
	idBytes := []byte("id-v1")
	copy(recordBuf[0:36], idBytes)
	copy(recordBuf[36:], ser1)
	f.Write(recordBuf)
	f.Close()

	// Now try loading using storage.Load
	loaded, err := storage.Load(filePath, tq)
	if err != nil {
		t.Fatalf("failed to load version 1 uncompressed file: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 loaded vector, got %d", len(loaded))
	}

	if _, ok := loaded["id-v1"]; !ok {
		t.Errorf("expected id-v1 to be loaded successfully from v1 format")
	}
}

