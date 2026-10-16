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

const readMaxLineBytes = 2000

func readTool() Tool {
	return Tool{
		Name:        "read",
		Description: "Read a text file, or see an image.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		Run:         runRead,
		View:        true,
		Summary:     func(args json.RawMessage) string { return argField(args, "path") },
	}
}

func runRead(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
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
	return string(data), nil
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
		Description:    "Create or overwrite a file, and its folders.",
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
		Description:    "Replace exact text in a file: old must be found once, or set all. edits: several at once, all or none.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"},
"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"}},"required":["old","new"]}}},"required":["path"]}`),
		Run:     runEdit,
		Summary: func(args json.RawMessage) string { return argField(args, "path") },
	}
}

type replacement struct {
	Old string `json:"old"`
	New string `json:"new"`
	All bool   `json:"all"`
}

func runEdit(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Path  string        `json:"path"`
		Old   *string       `json:"old"`
		New   *string       `json:"new"`
		All   bool          `json:"all"`
		Edits []replacement `json:"edits"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	edits := args.Edits
	if args.Old != nil || args.New != nil {
		if args.Old == nil || args.New == nil {
			return "", errors.New("give both old and new")
		}
		edits = append([]replacement{{Old: *args.Old, New: *args.New, All: args.All}}, edits...)
	}
	if len(edits) == 0 {
		return "", errors.New("give old and new, or edits")
	}
	path := env.Resolve(args.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	total := 0
	for i, edit := range edits {
		where := ""
		if len(edits) > 1 {
			where = fmt.Sprintf("edit %d: ", i+1)
		}
		if edit.Old == "" {
			return "", errors.New(where + "old is empty; use write to create a file")
		}
		count := strings.Count(content, edit.Old)
		if count == 0 {
			return "", errors.New(where + "old text not found")
		}
		if count > 1 && !edit.All {
			return "", fmt.Errorf("%sold text found %d times; add context to make it unique or set all", where, count)
		}
		content = strings.ReplaceAll(content, edit.Old, edit.New)
		total += count
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), info.Mode().Perm()); err != nil {
		return "", err
	}
	if len(edits) > 1 {
		return fmt.Sprintf("made %d edits, %d replacement(s)", len(edits), total), nil
	}
	return fmt.Sprintf("replaced %d occurrence(s)", total), nil
}
