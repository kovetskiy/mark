package utils

import (
	"os"
	"path/filepath"
	"strings"
)

func PathFinder(parent, relativePath, filePath string) (string, error) {
	var (
		spath string
		err   error
	)
	for {
		spath = filepath.Join(parent, relativePath, filePath)
		if _, err = os.Stat(spath); err == nil || os.IsExist(err) {
			return spath, nil
		}
		if relativePath == "" {
			break
		}
		if strings.Contains(relativePath, "/") {
			relativePath = relativePath[:strings.LastIndexAny(relativePath, "/")]
		} else {
			relativePath = ""
		}
	}
	return filePath, os.ErrNotExist
}
