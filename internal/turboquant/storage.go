package turboquant

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// Data Layout
// +----------------------+--------------------+-----------------------+---------------------------+
// |    magic (4-byte)    |  version (4-byte)  | len(vectors) (4-byte) | bytes_per_vector (4-byte) |
// +----------------------+--------------------+-----------------------+---------------------------+
// | dimensions (4 bytes) | bit width (1 byte) |
// +---------------------------+-----------------------------------------------------+------
// |    0. Row ID (36 bytes)   |   0. Serialised quantised vector (bytes_per_vector) |  ....
// +---------------------------+-----------------------------------------------------+------

const HeaderSize = 16

// Metadata size is 5 bytes: dimension uint32 + bit_width uint8
const MetadataSize = 5

type Storage struct {
	dimension      int
	bitWidth       int
	bytesPerVector int
}

func NewStorage(dimension, bitWidth int) *Storage {
	bytesPerVector := 36 + 4 + indexBytesNeeded(dimension, bitWidth)
	return &Storage{
		dimension:      dimension,
		bitWidth:       bitWidth,
		bytesPerVector: bytesPerVector,
	}
}

func (s *Storage) Load(filePath string, tq *TurboQuant) (map[string][]byte, error) {
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string][]byte), nil
		}
		return nil, err
	}
	defer f.Close()

	// 1. Read the entire 16-byte header uncompressed
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, fmt.Errorf("failed to read storage header: %w", err)
	}

	if string(header[0:4]) != "TQLM" {
		return nil, fmt.Errorf("invalid storage magic header")
	}

	version := binary.LittleEndian.Uint32(header[4:8])
	numVectors := int(binary.LittleEndian.Uint32(header[8:12]))
	bytesPerVector := int(binary.LittleEndian.Uint32(header[12:16]))

	// 2. Set up reader based on version (version 1: raw, version 2: zstd)
	var reader io.Reader = f
	if version == 2 {
		zr, err := zstd.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize zstd reader: %w", err)
		}
		defer zr.Close()
		reader = zr
	} else if version != 1 {
		return nil, fmt.Errorf("unsupported storage version: %d", version)
	}

	// 3. Read metadata from reader
	meta := make([]byte, MetadataSize)
	if _, err := io.ReadFull(reader, meta); err != nil {
		return nil, fmt.Errorf("failed to read storage metadata: %w", err)
	}

	// 4. Rebuild vectors map
	vectors := make(map[string][]byte, numVectors)
	buf := make([]byte, bytesPerVector)

	for i := range numVectors {
		_, err := io.ReadFull(reader, buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read vector record %d: %w", i, err)
		}

		// Extract ID and trim spaces/padding
		idEnd := 36
		for idEnd > 0 && buf[idEnd-1] == 0 {
			idEnd--
		}
		id := string(buf[0:idEnd])

		serialized := make([]byte, bytesPerVector-36)
		copy(serialized, buf[36:])

		vectors[id] = serialized
	}

	return vectors, nil
}

func (s *Storage) Save(filePath string, tq *TurboQuant, vectors map[string][]byte) error {
	// Re-write the file fresh to prevent accumulation of deleted items
	tmpFile := filePath + ".tmp"
	_ = os.Remove(tmpFile)

	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write header uncompressed (16 bytes)
	header := make([]byte, HeaderSize)
	copy(header[0:4], []byte("TQLM"))
	binary.LittleEndian.PutUint32(header[4:8], 2) // version 2 indicates zstd compressed body
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(vectors)))
	binary.LittleEndian.PutUint32(header[12:16], uint32(s.bytesPerVector))

	if _, err := f.Write(header); err != nil {
		return err
	}

	// Create zstd writer to compress metadata and vectors
	zw, err := zstd.NewWriter(f)
	if err != nil {
		return err
	}
	defer zw.Close()

	// Write metadata to zstd writer
	meta := make([]byte, MetadataSize)
	binary.LittleEndian.PutUint32(meta[0:4], uint32(s.dimension))
	meta[4] = uint8(s.bitWidth)

	if _, err := zw.Write(meta); err != nil {
		return err
	}

	// Write all records to zstd writer
	recordBuf := make([]byte, s.bytesPerVector)
	for id, serialized := range vectors {
		idBytes := make([]byte, 36)
		copy(idBytes, []byte(id))
		copy(recordBuf[0:36], idBytes)
		copy(recordBuf[36:], serialized)

		if _, err := zw.Write(recordBuf); err != nil {
			return err
		}
	}

	// Flush and close zstd writer first to ensure everything is flushed to f
	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to close zstd writer: %w", err)
	}

	f.Close()
	return os.Rename(tmpFile, filePath)
}
