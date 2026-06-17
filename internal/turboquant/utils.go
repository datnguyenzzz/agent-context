package turboquant

import (
	"fmt"
	"math"
	"math/rand"

	"gonum.org/v1/gonum/mat"
)

// Matrix represents a dense matrix, internally using gonum/mat.Dense.
type Matrix struct {
	data *mat.Dense
	dim  int
}

// Apply multiplies the matrix by a vector: result = M * vec.
func (m *Matrix) Apply(vec []float64) []float64 {
	out := make([]float64, m.dim)
	m.ApplyInto(vec, out)
	return out
}

// ApplyInto multiplies the matrix by a vector, writing the result into dst.
// dst must have length >= m.dim.
func (m *Matrix) ApplyInto(vec, dst []float64) {
	v := mat.NewVecDense(len(vec), vec)
	result := mat.NewVecDense(m.dim, dst[:m.dim])
	result.MulVec(m.data, v)
}

// ApplyTranspose multiplies the matrix transpose by a vector: result = M^T * vec.
func (m *Matrix) ApplyTranspose(vec []float64) []float64 {
	out := make([]float64, m.dim)
	m.ApplyTransposeInto(vec, out)
	return out
}

// ApplyTransposeInto multiplies the matrix transpose by a vector, writing the result into dst.
// dst must have length >= m.dim.
func (m *Matrix) ApplyTransposeInto(vec, dst []float64) {
	v := mat.NewVecDense(len(vec), vec)
	result := mat.NewVecDense(m.dim, dst[:m.dim])
	result.MulVec(m.data.T(), v)
}

// NewRandomOrthogonalMatrix generates a random orthogonal matrix.
// Obtained by QR decomposition of a random Gaussian matrix.
// Same seed produces the same matrix. Returns an error if dimension < 2.
func NewRandomOrthogonalMatrix(dimension int, seed int64) (*Matrix, error) {
	if err := ValidateDimension(dimension); err != nil {
		return nil, err
	}

	rng := rand.New(rand.NewSource(seed))

	// Generate dimension x dimension random Gaussian matrix
	data := make([]float64, dimension*dimension)
	for i := range data {
		data[i] = rng.NormFloat64()
	}
	gaussian := mat.NewDense(dimension, dimension, data)

	// QR decomposition
	var qr mat.QR
	qr.Factorize(gaussian)

	// Extract Q matrix
	var q mat.Dense
	qr.QTo(&q)

	return &Matrix{data: &q, dim: dimension}, nil
}

// Float64sToFloat32s converts a []float64 slice to []float32.
func Float64sToFloat32s(src []float64) []float32 {
	dst := make([]float32, len(src))
	for i, v := range src {
		dst[i] = float32(v)
	}
	return dst
}

// Float32sToFloat64s converts a []float32 slice to []float64.
func Float32sToFloat64s(src []float32) []float64 {
	dst := make([]float64, len(src))
	for i, v := range src {
		dst[i] = float64(v)
	}
	return dst
}

// IntsToFloat32s converts a []int slice to []float32.
func IntsToFloat32s(src []int) []float32 {
	dst := make([]float32, len(src))
	for i, v := range src {
		dst[i] = float32(v)
	}
	return dst
}

// Float32sToInts converts a []float32 slice back to []int by rounding.
func Float32sToInts(src []float32) []int {
	dst := make([]int, len(src))
	for i, v := range src {
		dst[i] = int(math.Round(float64(v)))
	}
	return dst
}

// BytesToFloat32s converts a []byte slice to []float32.
// Each byte value (0-255) is stored as a float32.
func BytesToFloat32s(src []byte) []float32 {
	dst := make([]float32, len(src))
	for i, v := range src {
		dst[i] = float32(v)
	}
	return dst
}

// Float32sToBytes converts a []float32 slice back to []byte by rounding and clamping to [0, 255].
func Float32sToBytes(src []float32) []byte {
	dst := make([]byte, len(src))
	for i, v := range src {
		r := math.Round(float64(v))
		if r < 0 {
			r = 0
		} else if r > 255 {
			r = 255
		}
		dst[i] = byte(r)
	}
	return dst
}

// StringToFloat32s converts a string to []float32 by treating each byte as a float32 value.
// This is a raw byte-level conversion, not a semantic embedding.
func StringToFloat32s(s string) []float32 {
	return BytesToFloat32s([]byte(s))
}

// Float32sToString converts a []float32 slice back to a string by rounding each value to a byte.
func Float32sToString(src []float32) string {
	return string(Float32sToBytes(src))
}

// BetaPDF computes the probability density of the Beta(alpha, beta) distribution at x.
// Uses log-space computation via math.Lgamma to avoid numerical overflow.
// Returns 0.0 for x outside the open interval (0, 1).
func BetaPDF(x, alpha, beta float64) float64 {
	if x <= 0 || x >= 1 {
		return 0.0
	}

	// log of the Beta function: B(α,β) = Γ(α)Γ(β)/Γ(α+β)
	lgA, _ := math.Lgamma(alpha)
	lgB, _ := math.Lgamma(beta)
	lgAB, _ := math.Lgamma(alpha + beta)
	logBeta := lgA + lgB - lgAB

	// log PDF = (α-1)*log(x) + (β-1)*log(1-x) - logB(α,β)
	logPDF := (alpha-1)*math.Log(x) + (beta-1)*math.Log(1-x) - logBeta

	return math.Exp(logPDF)
}

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns a float64 value in [-1, 1].
// Returns an error if the vectors have different dimensions.
// Returns 0.0 if either vector is a zero vector.
func CosineSimilarity(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("dimension mismatch: len(a)=%d, len(b)=%d", len(a), len(b))
	}
	if len(a) == 0 {
		return 0.0, nil
	}

	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 || normB == 0 {
		return 0.0, nil
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), nil
}

// CompressionRatio computes the theoretical compression ratio for a given dimension and bit width.
// Formula: (dimension * 32) / (32 + dimension * bitWidth)
// Original size: dimension * 32 bits (one float32 per element).
// Compressed size: 32 bits (float32 norm) + dimension * bitWidth bits.
func CompressionRatio(dimension, bitWidth int) float64 {
	return float64(dimension*32) / float64(32+dimension*bitWidth)
}
