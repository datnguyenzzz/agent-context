package turboquant

import (
	"fmt"
	"math"
)

// QuantizedVector represents a quantized vector, containing the original L2 norm
// and an array of quantization indices.
type QuantizedVector struct {
	Norm    float32 // L2 norm of the original vector
	Indices []uint8 // Quantization index for each coordinate
}

// BitWidth constants for supported quantization bit widths.
const (
	Bit2 = 2
	Bit3 = 3
	Bit4 = 4
)

// ValidateBitWidth returns an error if bitWidth is not one of the supported values (2, 3, or 4).
func ValidateBitWidth(bitWidth int) error {
	if bitWidth != Bit2 && bitWidth != Bit3 && bitWidth != Bit4 {
		return fmt.Errorf("invalid bit width %d: must be 2, 3, or 4", bitWidth)
	}
	return nil
}

// ValidateDimension returns an error if dimension is less than 2.
func ValidateDimension(dimension int) error {
	if dimension < 2 {
		return fmt.Errorf("invalid dimension %d: must be >= 2", dimension)
	}
	return nil
}

// quantizeVector quantizes a single float32 vector using the given rotation matrix and codebook.
// Steps: compute L2 norm → normalize → rotate → per-coordinate codebook lookup.
// Returns a QuantizedVector with the original norm and quantization indices.
// For zero vectors (norm == 0), returns norm=0 and all-zero indices.
func quantizeVector(vec []float32, rotation *Matrix, codebook *Codebook) (*QuantizedVector, error) {
	dim := len(vec)
	if dim != rotation.dim {
		return nil, fmt.Errorf("dimension mismatch: vector length %d, rotation matrix dimension %d", dim, rotation.dim)
	}

	// Compute L2 norm in float64 for precision.
	var sumSq float64
	for _, v := range vec {
		f := float64(v)
		sumSq += f * f
	}
	norm := math.Sqrt(sumSq)

	// Guard against NaN/Inf from extreme float32 values.
	if math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, fmt.Errorf("invalid vector: L2 norm is %v", norm)
	}

	// Handle zero vector: norm=0, all indices=0.
	if norm == 0 {
		return &QuantizedVector{
			Norm:    0,
			Indices: make([]uint8, dim),
		}, nil
	}

	// Normalize to unit vector in float64 (pooled temporary slice).
	normalized := getFloat64Slice(dim)
	for i, v := range vec {
		normalized[i] = float64(v) / norm
	}

	// Apply rotation into a pooled buffer: rotated = R · normalized.
	rotated := getFloat64Slice(dim)
	rotation.ApplyInto(normalized, rotated)
	putFloat64Slice(normalized) // done with normalized

	// Per-coordinate codebook lookup.
	indices := make([]uint8, dim)
	for i, val := range rotated {
		indices[i] = codebook.FindNearestIndex(val)
	}
	putFloat64Slice(rotated) // done with rotated

	return &QuantizedVector{
		Norm:    float32(norm),
		Indices: indices,
	}, nil
}

// dequantizeVector reconstructs a float32 vector from a QuantizedVector.
// Steps: look up centroid values by index → apply inverse rotation (transpose) → multiply by norm.
// Returns an error if indices length doesn't match rotation dimension or any index is out of bounds.
func dequantizeVector(qv *QuantizedVector, rotation *Matrix, codebook *Codebook) ([]float32, error) {
	if qv == nil {
		return nil, fmt.Errorf("quantized vector is nil")
	}

	dim := len(qv.Indices)
	if dim != rotation.dim {
		return nil, fmt.Errorf("dimension mismatch: indices length %d, rotation matrix dimension %d", dim, rotation.dim)
	}

	numCentroids := len(codebook.Centroids)

	// Look up centroid values into a pooled temporary slice.
	centroidValues := getFloat64Slice(dim)
	for i, idx := range qv.Indices {
		if int(idx) >= numCentroids {
			putFloat64Slice(centroidValues)
			return nil, fmt.Errorf("index out of bounds: index[%d]=%d, codebook has %d centroids", i, idx, numCentroids)
		}
		centroidValues[i] = codebook.Centroids[idx]
	}

	// Apply inverse rotation into a pooled buffer: R^T · centroidValues.
	restored := getFloat64Slice(dim)
	rotation.ApplyTransposeInto(centroidValues, restored)
	putFloat64Slice(centroidValues) // done with centroidValues

	// Multiply by norm and convert to float32.
	norm := float64(qv.Norm)
	result := make([]float32, dim)
	for i, v := range restored {
		result[i] = float32(v * norm)
	}
	putFloat64Slice(restored) // done with restored

	return result, nil
}
