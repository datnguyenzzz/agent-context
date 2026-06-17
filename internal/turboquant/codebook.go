package turboquant

import (
	"math"
	"sort"
	"sync"
)

// Codebook contains the centroids and partition boundaries of a Lloyd-Max quantizer.
type Codebook struct {
	Centroids  []float64 // 2^BitWidth centroids, sorted ascending
	Boundaries []float64 // 2^BitWidth - 1 partition boundaries
	BitWidth   int
}

// FindNearestIndex finds the index of the nearest centroid for the given value
// using binary search on the partition boundaries.
// The boundaries divide the real line into intervals, each mapped to a centroid index.
// Interval mapping: (-inf, b[0]) -> 0, [b[0], b[1]) -> 1, ..., [b[n-1], +inf) -> n
func (c *Codebook) FindNearestIndex(value float64) uint8 {
	// Use sort.Search with strict less-than so that a value exactly equal to a
	// boundary is assigned to the upper interval (the one starting at that boundary).
	idx := sort.Search(len(c.Boundaries), func(i int) bool {
		return c.Boundaries[i] > value
	})
	return uint8(idx)
}

// CodebookBuilder constructs a Codebook by running Lloyd-Max optimization
// on the Beta distribution derived from the vector dimension.
type CodebookBuilder struct {
	gridPoints int // number of grid points for numerical integration, default 50000
	iterations int // number of Lloyd-Max iterations, default 300
}

// NewCodebookBuilder returns a CodebookBuilder with default parameters
// (gridPoints=50000, iterations=300).
func NewCodebookBuilder() *CodebookBuilder {
	return &CodebookBuilder{
		gridPoints: 50000,
		iterations: 300,
	}
}

// Build constructs a Codebook for the given dimension and bitWidth using
// Lloyd-Max optimization on the Beta((d-1)/2, (d-1)/2) distribution.
//
// The Beta distribution is defined on (0,1) and mapped to (-1,1) via x_mapped = 2*x - 1.
// Returns an error if bitWidth is not 2, 3, or 4, or if dimension < 2.
func (cb *CodebookBuilder) Build(dimension, bitWidth int) (*Codebook, error) {
	if err := ValidateBitWidth(bitWidth); err != nil {
		return nil, err
	}
	if err := ValidateDimension(dimension); err != nil {
		return nil, err
	}

	numCentroids := 1 << bitWidth // 2^bitWidth

	// Beta distribution parameters
	alpha := float64(dimension-1) / 2.0
	beta := alpha // symmetric Beta

	// Compute mean and std of the Beta distribution to focus the grid
	betaMean := alpha / (alpha + beta)
	betaVar := (alpha * beta) / ((alpha + beta) * (alpha + beta) * (alpha + beta + 1))
	betaStd := math.Sqrt(betaVar)

	// Grid range: mean ± 6 standard deviations, clamped to (0, 1)
	gridLo := math.Max(1e-10, betaMean-6*betaStd)
	gridHi := math.Min(1-1e-10, betaMean+6*betaStd)

	// Create grid points on (gridLo, gridHi)
	n := cb.gridPoints
	gridPDF := make([]float64, n) // Beta PDF values
	gridMap := make([]float64, n) // mapped values in (-1,1)
	step := (gridHi - gridLo) / float64(n-1)

	for i := 0; i < n; i++ {
		x := gridLo + float64(i)*step
		gridPDF[i] = BetaPDF(x, alpha, beta)
		gridMap[i] = 2*x - 1 // map (0,1) -> (-1,1)
	}

	// Initialize centroids uniformly spaced in [-1, 1]
	centroids := make([]float64, numCentroids)
	for i := 0; i < numCentroids; i++ {
		centroids[i] = -1.0 + (2.0*float64(i)+1.0)/float64(numCentroids)
	}

	// Boundaries array: numCentroids - 1 boundaries
	boundaries := make([]float64, numCentroids-1)

	// Pre-allocate newCentroids once and reuse across iterations (task 26.2:
	// avoid per-iteration allocation in the hot loop).
	newCentroids := make([]float64, numCentroids)

	// Lloyd-Max iterations
	for iter := 0; iter < cb.iterations; iter++ {
		// Update boundaries: midpoint of adjacent centroids
		for i := 0; i < numCentroids-1; i++ {
			boundaries[i] = (centroids[i] + centroids[i+1]) / 2.0
		}

		// Update centroids: weighted mean of mapped grid values in each interval.
		// gridMap is sorted ascending (uniform grid mapped linearly), so we use
		// binary search to find the sub-range of grid points within [lo, hi)
		// instead of scanning all n points for each centroid (task 26.3).
		for k := 0; k < numCentroids; k++ {
			var lo, hi float64
			if k == 0 {
				lo = -1.0 // -inf mapped, but grid is bounded
			} else {
				lo = boundaries[k-1]
			}
			if k == numCentroids-1 {
				hi = 1.0 // +inf mapped, but grid is bounded
			} else {
				hi = boundaries[k]
			}

			// Binary search for the first grid point >= lo
			jStart := sort.SearchFloat64s(gridMap, lo)
			// Binary search for the first grid point >= hi
			jEnd := sort.SearchFloat64s(gridMap, hi)

			var weightedSum, weightSum float64
			for j := jStart; j < jEnd; j++ {
				w := gridPDF[j]
				weightedSum += gridMap[j] * w
				weightSum += w
			}

			if weightSum > 0 {
				newCentroids[k] = weightedSum / weightSum
			} else {
				// Keep old centroid if no grid points fall in this interval
				newCentroids[k] = centroids[k]
			}
		}
		// Swap centroids and newCentroids to reuse the buffer
		centroids, newCentroids = newCentroids, centroids
	}

	// Final boundary computation
	for i := 0; i < numCentroids-1; i++ {
		boundaries[i] = (centroids[i] + centroids[i+1]) / 2.0
	}

	// Ensure centroids are sorted ascending (they should be by construction)
	sort.Float64s(centroids)

	return &Codebook{
		Centroids:  centroids,
		Boundaries: boundaries,
		BitWidth:   bitWidth,
	}, nil
}

// codebookKey is the cache key for codebook lookup.
type codebookKey struct {
	dimension int
	bitWidth  int
}

// codebookCache stores previously built codebooks keyed by (dimension, bitWidth).
var codebookCache sync.Map

// GetOrBuildCodebook returns a cached Codebook for the given parameters,
// or builds a new one using NewCodebookBuilder().Build and caches it.
// Thread-safe via sync.Map.
func GetOrBuildCodebook(dimension, bitWidth int) (*Codebook, error) {
	key := codebookKey{dimension: dimension, bitWidth: bitWidth}

	if cached, ok := codebookCache.Load(key); ok {
		return cached.(*Codebook), nil
	}

	cb, err := NewCodebookBuilder().Build(dimension, bitWidth)
	if err != nil {
		return nil, err
	}

	actual, _ := codebookCache.LoadOrStore(key, cb)
	return actual.(*Codebook), nil
}

// ResetCodebookCache clears the global codebook cache. Intended for testing.
func ResetCodebookCache() {
	codebookCache.Range(func(key, value any) bool {
		codebookCache.Delete(key)
		return true
	})
}
