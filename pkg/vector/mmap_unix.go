//go:build unix

package vector

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// mapFloats maps path read-only and reinterprets it as a []float64.
//
// The reinterpretation is sound because mmap returns a page-aligned address —
// far stricter than float64's 8-byte requirement — and because vectors.bin is
// written in host byte order, which Index.Usable has already checked. An empty
// file maps to a nil slice rather than failing: a graph with no vectors is a
// legitimate state.
func mapFloats(path string) ([]float64, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	size := info.Size()
	if size == 0 {
		return nil, func() error { return nil }, nil
	}
	if size%8 != 0 {
		return nil, nil, fmt.Errorf("%s: %d bytes is not a whole number of float64", path, size)
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("mmap %s: %w", path, err)
	}

	floats := unsafe.Slice((*float64)(unsafe.Pointer(&data[0])), size/8)
	return floats, func() error { return syscall.Munmap(data) }, nil
}
