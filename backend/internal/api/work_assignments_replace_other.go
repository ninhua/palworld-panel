//go:build !windows

package api

import "os"

func atomicReplaceLevelFile(source, destination string) error {
	return os.Rename(source, destination)
}
