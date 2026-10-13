package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/jg/mo/internal/render"
)

const (
	readDefaultLimit = 2000
	readMaxLineBytes = 2000
)

func readTool() Tool {
	return Tool{
		Name:        "read",
		Description: "Read a text file. offset is the first line (1-based).",
		Schema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`),
		Run:     runRead,
		Summary: func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runRead(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	path := env.Resolve(args.Path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if render.IsImageFile(path) {
		return describeImage(path, data)
	}
	if looksBinary(data) {
		return "", errors.New("binary file")
	}
	if len(data) == 0 {
		return "(empty file)", nil
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	first := max(args.Offset, 1)
	if first > len(lines) {
		return "", fmt.Errorf("offset %d is past the end (%d lines)", first, len(lines))
	}
	limit := args.Limit
	if limit <= 0 {
		limit = readDefaultLimit
	}
	last := min(first-1+limit, len(lines))

	var out strings.Builder
	for _, line := range lines[first-1 : last] {
		if len(line) > readMaxLineBytes {
			line = cutAtRune(line, readMaxLineBytes) + " [line cut]"
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if last < len(lines) {
		fmt.Fprintf(&out, "[lines %d-%d of %d; continue with offset=%d]\n", first, last, len(lines), last+1)
	}
	return out.String(), nil
}

// describeImage answers a read of an image with its size and a marker, so
// the user sees it and the model knows what it is.
func describeImage(path string, data []byte) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		return render.ImageMarker(fmt.Sprintf("svg, %d KB", len(data)>>10), path), nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("unreadable image: %w", err)
	}
	return render.ImageMarker(fmt.Sprintf("%s %dx%d", format, config.Width, config.Height), path), nil
}

func looksBinary(data []byte) bool {
	sample := data[:min(len(data), 8000)]
	return bytes.IndexByte(sample, 0) >= 0
}

func writeTool() Tool {
	return Tool{
		ChangesPathArg: true,
		Name:           "write",
		Description:    "Create or overwrite a file. Creates parent directories.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		Run:     runWrite,
		Summary: func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runWrite(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	path := env.Resolve(args.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes", len(args.Content)), nil
}

func editTool() Tool {
	return Tool{
		ChangesPathArg: true,
		Name:           "edit",
		Description:    "Replace exact text in a file. old must match once unless all is true.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"}},"required":["path","old","new"]}`),
		Run:     runEdit,
		Summary: func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runEdit(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
		All  bool   `json:"all"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	if args.Old == "" {
		return "", errors.New("old is empty; use write to create a file")
	}
	path := env.Resolve(args.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, args.Old)
	if count == 0 {
		return "", errors.New("old text not found")
	}
	if count > 1 && !args.All {
		return "", fmt.Errorf("old text found %d times; add context to make it unique or set all", count)
	}
	updated := strings.ReplaceAll(content, args.Old, args.New)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced %d occurrence(s)", count), nil
}
