//go:build !windows

package main

import (
	"fmt"
	"math"
	"syscall"
)

func firmwareTempFreeBytes(root string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return 0, fmt.Errorf("statfs firmware temp dir: %w", err)
	}
	if stat.Bsize <= 0 {
		return 0, nil
	}
	blockSize := uint64(stat.Bsize) //nolint:gosec // Bsize is guarded above; Statfs reports a non-negative block size in practice.
	availableBlocks := stat.Bavail
	if availableBlocks > uint64(math.MaxInt64)/blockSize {
		return math.MaxInt64, nil
	}
	return int64(availableBlocks * blockSize), nil //nolint:gosec // The division guard bounds the product to MaxInt64.
}
