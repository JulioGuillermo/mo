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
	"sort"
	"strconv"
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
	var changed []span
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
		content, changed = replaceTracked(content, edit.Old, edit.New, changed)
		total += count
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), info.Mode().Perm()); err != nil {
		return "", err
	}
	at := "at " + lineList(content, changed)
	if len(edits) > 1 {
		return fmt.Sprintf("made %d edits, %d replacement(s), %s", len(edits), total, at), nil
	}
	return fmt.Sprintf("replaced %d occurrence(s), %s", total, at), nil
}

// span is where a replacement's new text is in the content, by byte.
type span struct{ start, end int }

// replaceTracked replaces every old with new, adding where each new text
// went to spans, and moving the spans already there by what changed
// before them.
func replaceTracked(content, old, new string, spans []span) (string, []span) {
	var out strings.Builder
	var made, at []int
	last := 0
	for {
		found := strings.Index(content[last:], old)
		if found < 0 {
			break
		}
		found += last
		out.WriteString(content[last:found])
		made = append(made, out.Len())
		at = append(at, found)
		out.WriteString(new)
		last = found + len(old)
	}
	out.WriteString(content[last:])
	grow := len(new) - len(old)
	for i, earlier := range spans {
		before := 0
		for _, found := range at {
			if found < earlier.start {
				before++
			}
		}
		spans[i] = span{earlier.start + before*grow, max(earlier.end+before*grow, earlier.start+before*grow)}
	}
	for _, start := range made {
		spans = append(spans, span{start, start + len(new)})
	}
	return out.String(), spans
}

// lineList names the lines the spans are on, as "lines 3, 7-9 and 20".
func lineList(content string, spans []span) string {
	lineAt := func(offset int) int { return strings.Count(content[:min(max(offset, 0), len(content))], "\n") + 1 }
	var ranges [][2]int
	for _, each := range spans {
		first := lineAt(each.start)
		ranges = append(ranges, [2]int{first, max(lineAt(each.end-1), first)})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	var merged [][2]int
	for _, each := range ranges {
		if n := len(merged); n > 0 && each[0] <= merged[n-1][1]+1 {
			merged[n-1][1] = max(merged[n-1][1], each[1])
			continue
		}
		merged = append(merged, each)
	}
	var names []string
	for i, each := range merged {
		if i == 10 {
			names = append(names, "more")
			break
		}
		name := strconv.Itoa(each[0])
		if each[1] > each[0] {
			name += "-" + strconv.Itoa(each[1])
		}
		names = append(names, name)
	}
	word := "lines "
	if len(merged) == 1 && merged[0][0] == merged[0][1] {
		word = "line "
	}
	if len(names) == 1 {
		return word + names[0]
	}
	return word + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
