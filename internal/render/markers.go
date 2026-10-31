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

var mediaMarker = regexp.MustCompile(`\[(image|audio|video) [^\]:]*: (/[^\]\s]+|[A-Za-z]:\\[^\]\n]+)\]`)

// MediaMarker is how a tool result points at an image, a recording or a
// video: the model reads what it is and where, and gets the file with it.
func MediaMarker(kind, description, path string) string {
	return fmt.Sprintf("[%s %s: %s]", kind, description, path)
}

// ImageMarker is the MediaMarker of an image, which the UI also draws below
// the call.
func ImageMarker(description, path string) string {
	return MediaMarker("image", description, path)
}

// MarkedImages are the paths of the images a text marks.
func MarkedImages(text string) []string {
	var paths []string
	for _, match := range mediaMarker.FindAllStringSubmatch(text, -1) {
		if match[1] == "image" {
			paths = append(paths, match[2])
		}
	}
	return paths
}

// MarkedMedia are the paths of the images, recordings and videos a text
// marks.
func MarkedMedia(text string) []string {
	var paths []string
	for _, match := range mediaMarker.FindAllStringSubmatch(text, -1) {
		paths = append(paths, match[2])
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
