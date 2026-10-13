package render

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const maxFileBytes = 20 << 20

var imageExtensions = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"}

// File draws an image, SVG or Mermaid file.
func (r Renderer) File(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return "", fmt.Errorf("%s: not a regular file under %d MB", path, maxFileBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	switch extension := strings.ToLower(filepath.Ext(path)); {
	case extension == ".svg":
		return r.SVG(data)
	case extension == ".mmd" || extension == ".mermaid":
		return r.Mermaid(string(data)), nil
	}
	return r.ImageData(data)
}

func IsImageFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".svg" || slices.Contains(imageExtensions, extension)
}
