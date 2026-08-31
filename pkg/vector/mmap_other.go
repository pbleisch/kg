//go:build !unix

package vector

import (
	"fmt"
	"math"
	"os"
)

// mapFloats reads path into memory and decodes it as float64s.
//
// This is the portable fallback for platforms without syscall.Mmap. It costs
// one copy of the sidecar at open time; the search loop that follows is
// identical, because both paths hand back the same []float64.
func mapFloats(path string) ([]float64, func() error, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, func() error { return nil }, nil
	}
	if len(data)%8 != 0 {
		return nil, nil, fmt.Errorf("%s: %d bytes is not a whole number of float64", path, len(data))
	}
	floats := make([]float64, len(data)/8)
	for i := range floats {
		floats[i] = math.Float64frombits(nativeEndian.Uint64(data[i*8:]))
	}
	return floats, func() error { return nil }, nil
}
