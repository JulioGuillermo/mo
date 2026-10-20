package tools

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

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
		ViewSelf:    true,
		Summary:     func(args json.RawMessage) string { return argField(args, "path") },
	}
}

// readSniff is how much of a file read looks at to tell text from binary.
const readSniff = 8000

func runRead(ctx context.Context, env Env, raw json.RawMessage) (string, error) {
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
	if render.IsImageFile(path) {
		return describeImage(path, info.Size())
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	head := make([]byte, readSniff)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	head = head[:n]
	size := info.Size()
	view, given := ViewFrom(ctx)
	tooBig := env.ReadMaxSize > 0 && size > env.ReadMaxSize
	if tooBig && !given {
		return "", fmt.Errorf("file is %s, over tools.read_max_size (%s): read a part with offset and limit (offset -100 for the last lines) or match, or use %s",
			formatSize(size), formatSize(env.ReadMaxSize), shellHint(hintPart))
	}
	if order, ok := utf16Order(head); ok {
		if tooBig {
			return "", fmt.Errorf("file is UTF-16 and %s, over tools.read_max_size (%s): use %s", formatSize(size), formatSize(env.ReadMaxSize), shellHint(hintUTF16))
		}
		rest, err := io.ReadAll(file)
		if err != nil {
			return "", err
		}
		return view.Apply(decodeUTF16(append(head, rest...), order)), nil
	}
	if kind, binary := sniffBinary(head); binary {
		return "", fmt.Errorf("binary file (%s, %s): read takes text; use %s", kind, formatSize(size), shellHint(hintBinary))
	}
	if tooBig {
		return readPart(ctx, file, head, view)
	}
	rest, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	data := append(head, rest...)
	if len(data) == 0 {
		return "(empty file)", nil
	}
	return view.Apply(string(data)), nil
}

// readPart shows a view of a file too big to hold, reading it a line at a
// time: twice for a negative offset, first to count the lines.
func readPart(ctx context.Context, file *os.File, head []byte, view View) (string, error) {
	if view.Offset < 0 {
		counter := view
		counter.Offset, counter.Limit = 0, 0
		picker := counter.picker()
		if err := eachLine(ctx, io.MultiReader(bytes.NewReader(head), file), view.Col+view.Width, picker.each); err != nil {
			return "", err
		}
		if _, err := file.Seek(int64(len(head)), io.SeekStart); err != nil {
			return "", err
		}
		view.Offset = max(picker.seen+view.Offset, 0)
	}
	picker := view.picker()
	if err := eachLine(ctx, io.MultiReader(bytes.NewReader(head), file), view.Col+view.Width, picker.each); err != nil {
		return "", err
	}
	return picker.result(), nil
}

// readKeepLine is how much of a line readPart holds, besides what the view
// shows of it: a match looks only this far into a longer one.
const readKeepLine = 1 << 20

// eachLine hands each line of r to add with its number and length, holding
// at most readKeepLine bytes of it, or keep if more, until add says to stop;
// ctx stops it too.
func eachLine(ctx context.Context, r io.Reader, keep int, add func(number int, line string, full int) bool) error {
	keep = max(keep, readKeepLine)
	reader := bufio.NewReaderSize(r, 64<<10)
	var line []byte
	full, number := 0, 0
	for {
		chunk, err := reader.ReadSlice('\n')
		full += len(chunk)
		if room := keep - len(line); room > 0 {
			line = append(line, chunk[:min(len(chunk), room)]...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if full > 0 {
			text := strings.TrimSuffix(string(line), "\n")
			length := full
			if bytes.HasSuffix(chunk, []byte("\n")) {
				length--
			}
			if !add(number, text, length) {
				return nil
			}
			number++
		}
		if err != nil {
			return nil
		}
		if number%4096 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		line, full = line[:0], 0
	}
}

// describeImage answers a read of an image with its size and a marker, so
// the user sees it and the model knows what it is; only its header is read.
func describeImage(path string, size int64) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		return render.ImageMarker(fmt.Sprintf("svg, %d KB", size>>10), path), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(bufio.NewReader(file))
	if err != nil {
		return "", fmt.Errorf("unreadable image: %w", err)
	}
	return render.ImageMarker(fmt.Sprintf("%s %dx%d", format, config.Width, config.Height), path), nil
}

// sniffBinary tells binary data by the start of a file: a NUL byte, or
// more than a tenth of it control characters or not UTF-8. kind is what it
// looks like.
func sniffBinary(head []byte) (kind string, binary bool) {
	kind = http.DetectContentType(head)
	if strings.HasPrefix(kind, "text/") || kind == "application/octet-stream" {
		kind = "binary data"
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return kind, true
	}
	odd, total := 0, 0
	for i := 0; i < len(head); {
		r, width := utf8.DecodeRune(head[i:])
		if r == utf8.RuneError && width <= 1 && !utf8.FullRune(head[i:]) {
			break // a character cut where the sample ends
		}
		total++
		switch {
		case r == utf8.RuneError && width <= 1:
			odd++
		case r < 0x20 && !strings.ContainsRune("\t\n\r\f\v\x1b", r), r == 0x7f:
			odd++
		}
		i += max(width, 1)
	}
	return kind, odd*10 > total
}

// utf16Order is the byte order a UTF-16 file's mark says it has.
func utf16Order(head []byte) (binary.ByteOrder, bool) {
	switch {
	case bytes.HasPrefix(head, []byte{0xff, 0xfe}):
		return binary.LittleEndian, true
	case bytes.HasPrefix(head, []byte{0xfe, 0xff}):
		return binary.BigEndian, true
	}
	return nil, false
}

func decodeUTF16(data []byte, order binary.ByteOrder) string {
	data = data[2:]
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[2*i:])
	}
	return strings.ReplaceAll(string(utf16.Decode(units)), "\r\n", "\n")
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
		Description: "Replace exact text: old must be found once, or set all to replace every one.\n" +
			"One change: {path, old, new}.\n" +
			"Several in one file: {path, edits: [{old, new}, ...]}.\n" +
			"Several files: {edits: [{path, old, new}, ...]}.\n" +
			"All are made or none.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"},
"edits":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"all":{"type":"boolean"}},"required":["old","new"]}}}}`),
		Run:     runEdit,
		Summary: func(args json.RawMessage) string { return strings.Join(editedPaths(args), ", ") },
	}
}

type replacement struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
	All  bool   `json:"all"`
}

// editedFile is a file an edit changes: as it was, and as the edits leave
// it, with where the new text went.
type editedFile struct {
	path     string
	shown    string
	before   []byte
	content  string
	mode     os.FileMode
	edits    int
	replaced int
	changed  []span
}

// runEdit makes every edit in memory first, each in its own path or the
// call's, and writes the files only when all of them apply; a write that
// fails puts back the files written before it.
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
	var files []*editedFile
	byPath := map[string]*editedFile{}
	for i, edit := range edits {
		where := ""
		if len(edits) > 1 {
			where = fmt.Sprintf("edit %d: ", i+1)
		}
		shown := cmp.Or(edit.Path, args.Path)
		if shown == "" {
			return "", errors.New(where + "give path, for the call or for the edit")
		}
		path := env.Resolve(shown)
		file, opened := byPath[path]
		if !opened {
			var err error
			if file, err = openForEdit(path, shown); err != nil {
				return "", errors.New(where + err.Error())
			}
			byPath[path] = file
			files = append(files, file)
		}
		if edit.Old == "" {
			return "", errors.New(where + "old is empty; use write to create a file")
		}
		count := strings.Count(file.content, edit.Old)
		if count == 0 {
			return "", fmt.Errorf("%sold text not found in %s", where, shown)
		}
		if count > 1 && !edit.All {
			return "", fmt.Errorf("%sold text found %d times in %s; add context to make it unique or set all", where, count, shown)
		}
		file.content, file.changed = replaceTracked(file.content, edit.Old, edit.New, file.changed)
		file.edits++
		file.replaced += count
	}
	if err := writeAll(files); err != nil {
		return "", err
	}
	if len(files) == 1 {
		return editReport(files[0]), nil
	}
	var lines []string
	for _, file := range files {
		lines = append(lines, file.shown+": "+editReport(file))
	}
	return fmt.Sprintf("edited %d files\n%s", len(files), strings.Join(lines, "\n")), nil
}

func openForEdit(path, shown string) (*editedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &editedFile{path: path, shown: shown, before: data, content: string(data), mode: info.Mode().Perm()}, nil
}

// writeAll writes the files, or, when one fails, puts back those written.
func writeAll(files []*editedFile) error {
	for i, file := range files {
		if err := os.WriteFile(file.path, []byte(file.content), file.mode); err != nil {
			for _, written := range files[:i] {
				os.WriteFile(written.path, written.before, written.mode)
			}
			return fmt.Errorf("%s: %w; no file was changed", file.shown, err)
		}
	}
	return nil
}

func editReport(file *editedFile) string {
	at := "at " + lineList(file.content, file.changed)
	if file.edits > 1 {
		return fmt.Sprintf("made %d edits, %d replacement(s), %s", file.edits, file.replaced, at)
	}
	return fmt.Sprintf("replaced %d occurrence(s), %s", file.replaced, at)
}

// editedPaths are the files an edit call changes, each once, in order.
func editedPaths(args json.RawMessage) []string {
	var fields struct {
		Path  string  `json:"path"`
		Old   *string `json:"old"`
		Edits []struct {
			Path string `json:"path"`
		} `json:"edits"`
	}
	json.Unmarshal(args, &fields)
	var paths []string
	add := func(path string) {
		if path != "" && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	if fields.Old != nil || len(fields.Edits) == 0 {
		add(fields.Path)
	}
	for _, edit := range fields.Edits {
		add(cmp.Or(edit.Path, fields.Path))
	}
	return paths
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

// formatSize is a size as people read it: 3.2 GB, 640 KB, 12 bytes.
func formatSize(size int64) string {
	switch {
	case size >= 1<<30:
		return trimZero(float64(size)/(1<<30)) + " GB"
	case size >= 1<<20:
		return trimZero(float64(size)/(1<<20)) + " MB"
	case size >= 1<<10:
		return trimZero(float64(size)/(1<<10)) + " KB"
	}
	return fmt.Sprintf("%d bytes", size)
}

func trimZero(value float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0")
}
