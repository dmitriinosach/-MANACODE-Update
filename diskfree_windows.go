package main

import (
	"math"

	"golang.org/x/sys/windows"
)

func diskFree(dir string) (int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return int64(min(free, math.MaxInt64)), nil
}
