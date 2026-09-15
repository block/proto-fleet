package main

import (
	"fmt"
	"math"

	"golang.org/x/sys/windows"
)

func firmwareTempFreeBytes(root string) (int64, error) {
	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, fmt.Errorf("firmware temp path: %w", err)
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("free space in firmware temp dir: %w", err)
	}
	return int64(min(available, math.MaxInt64)), nil
}
