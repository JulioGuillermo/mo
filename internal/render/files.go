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

// recordingMIME are the audio and video files models take, by extension.
var recordingMIME = map[string]string{
	".wav": "audio/wav", ".mp3": "audio/mpeg", ".flac": "audio/flac", ".ogg": "audio/ogg", ".m4a": "audio/mp4",
	".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska", ".avi": "video/x-msvideo",
}

// RecordingMIME is the MIME type of an audio or video file, empty for any
// other file.
func RecordingMIME(path string) string {
	return recordingMIME[strings.ToLower(filepath.Ext(path))]
}

func IsImageFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".svg" || slices.Contains(imageExtensions, extension)
}
