//go:build windows

package artifact

import "os"

func openArtifactFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidKey
	}
	return os.Open(path)
}
