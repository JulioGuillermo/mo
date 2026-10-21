package render

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// writeNew creates the file, or keeps it when it is already there: the name
// is the content's hash, so the same name holds the same data.
func writeNew(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

var extensionByMIME = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif",
	"image/webp": ".webp", "image/bmp": ".bmp", "image/svg+xml": ".svg",
}

var imageMarker = regexp.MustCompile(`\[image [^\]:]*: (/[^\]\s]+|[A-Za-z]:\\[^\]\n]+)\]`)

// ImageMarker is how a tool result points at an image: the model reads what
// it is and where, and the UI draws it below the call.
func ImageMarker(description, path string) string {
	return fmt.Sprintf("[image %s: %s]", description, path)
}

func MarkedImages(text string) []string {
	var paths []string
	for _, match := range imageMarker.FindAllStringSubmatch(text, -1) {
		paths = append(paths, match[1])
	}
	return paths
}

// imageDir is a private directory made for this run, so no other user can
// read the images or plant files in it.
var imageDir = sync.OnceValues(newImageDir)

func newImageDir() (string, error) {
	return os.MkdirTemp("", "mo-images-")
}

// RemoveSavedImages deletes the images saved in this run.
func RemoveSavedImages() {
	if dir, err := imageDir(); err == nil {
		os.RemoveAll(dir)
	}
}

// SaveImage writes image data a tool returned to a file named after its
// content, and returns the marker that points at it.
func SaveImage(data []byte, mimeType string) (string, error) {
	dir, err := imageDir()
	if err != nil {
		return "", err
	}
	extension, known := extensionByMIME[mimeType]
	if !known {
		extension = ".png"
	}
	name := fmt.Sprintf("%x", sha256.Sum256(data))[:16] + extension
	path := filepath.Join(dir, name)
	if err := writeNew(path, data); err != nil {
		return "", err
	}
	return ImageMarker(fmt.Sprintf("%s, %d KB", mimeType, len(data)>>10), path), nil
}
