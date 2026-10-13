package render

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | RD-01 | Happy    | Split separates text, mermaid and svg blocks and local image lines   | P0       |
// | RD-02 | Edge     | Remote images stay in the text; an open block runs to the end        | P1       |
// | RD-03 | Happy    | Kitty placeholders: one line per row, id in the colour, row marks    | P0       |
// | RD-04 | Happy    | The kitty transmission is chunked, quiet, and a virtual placement    | P0       |
// | RD-05 | Edge     | Inside tmux the transmission is wrapped for passthrough              | P1       |
// | RD-06 | Happy    | Half blocks give one text row per two pixel rows, width as the box   | P1       |
// | RD-07 | Happy    | fit keeps proportions, the column limit and never enlarges           | P1       |
// | RD-08 | Happy    | Detect picks kitty for kitty/ghostty, blocks for truecolor, else off | P1       |
// | RD-09 | Happy    | A saved image is marked, found again, and has a usable extension     | P1       |
// | RD-10 | Happy    | Mermaid draws with box characters; what it cannot draw stays source  | P1       |
// | RD-11 | Happy    | SVG text is drawn with an installed font, also for generic families  | P1       |
// | RD-12 | Happy    | With images off an image is named, not drawn                         | P2       |
// | RD-13 | Edge     | Inside another code block, image lines and mermaid fences stay text   | P0       |
// | RD-14 | Security | An image whose header declares too many pixels is refused undecoded  | P0       |
// | RD-15 | Error    | Malformed SVG is refused before the renderer sees it                 | P1       |
// | RD-16 | Happy    | Text is drawn for quoted, single-quoted, style and CSS font families | P1       |
// | RD-17 | Happy    | A huge SVG is drawn straight at the size it is shown                  | P1       |
// | RD-18 | Happy    | Kitty sends the image at its size on screen, not its full resolution | P1       |
// | RD-19 | Security | Saved images live in a private directory removed at the end          | P0       |
// | RD-20 | Edge     | Inside tmux without passthrough, kitty falls back to half blocks     | P1       |
// | RD-21 | Happy    | width="100%" with a viewBox keeps the viewBox's proportions          | P1       |
// | RD-22 | Happy    | An SVG without xmlns is drawn; one in another namespace is refused   | P1       |
// | RD-23 | Happy    | Absolute units convert to pixels; em alone falls back to a default   | P2       |

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func solid(width, height int, fill color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, fill)
		}
	}
	return img
}

func TestSplit(t *testing.T) {
	t.Run("RD-01 Split separates text, mermaid and svg blocks and local image lines", func(t *testing.T) {
		markdown := "Intro\n```mermaid\ngraph LR\n  A --> B\n```\nMiddle\n![logo](docs/logo.png)\n~~~svg\n<svg/>\n~~~\nEnd\n"

		segments := Split(markdown)

		kinds := []SegmentKind{SegmentText, SegmentMermaid, SegmentText, SegmentImageFile, SegmentSVG, SegmentText}
		if len(segments) != len(kinds) {
			t.Fatalf("segments = %+v", segments)
		}
		for i, kind := range kinds {
			if segments[i].Kind != kind {
				t.Errorf("segment %d = %+v", i, segments[i])
			}
		}
		if segments[1].Content != "graph LR\n  A --> B\n" || segments[3].Content != "docs/logo.png" || segments[4].Content != "<svg/>\n" {
			t.Errorf("contents = %q %q %q", segments[1].Content, segments[3].Content, segments[4].Content)
		}
	})

	t.Run("RD-02 remote images stay in the text; an open block runs to the end", func(t *testing.T) {
		segments := Split("![x](https://example.com/a.png)\n```mermaid\ngraph TD\n  A --> B\n")

		if len(segments) != 2 || segments[0].Kind != SegmentText || segments[1].Kind != SegmentMermaid {
			t.Errorf("segments = %+v", segments)
		}
	})
}

func TestKitty(t *testing.T) {
	t.Run("RD-03 kitty placeholders: one line per row, id in the colour, row marks", func(t *testing.T) {
		text := kittyPlaceholders(0x010203, Box{Cols: 4, Rows: 3})

		lines := strings.Split(text, "\n")
		if len(lines) != 3 {
			t.Fatalf("lines = %d", len(lines))
		}
		for row, line := range lines {
			if !strings.HasPrefix(line, "\x1b[38;2;1;2;3m") || strings.Count(line, string(placeholder)) != 4 {
				t.Errorf("row %d = %q", row, line)
			}
			if !strings.Contains(line, string([]rune{placeholder, diacritics[row], diacritics[0]})) {
				t.Errorf("row %d lacks its marks", row)
			}
			if width := ansi.StringWidth(line); width != 4 {
				t.Errorf("row %d is %d cells wide", row, width)
			}
		}
	})

	t.Run("RD-04 the kitty transmission is chunked, quiet, and a virtual placement", func(t *testing.T) {
		t.Setenv("TMUX", "")
		payload := strings.Repeat("A", kittyChunkBytes*2+10)

		text := kittyTransmission(payload, 7, Box{Cols: 5, Rows: 2})

		chunks := strings.Split(strings.TrimSuffix(text, "\x1b\\"), "\x1b\\")
		if len(chunks) != 3 {
			t.Fatalf("chunks = %d", len(chunks))
		}
		if !strings.HasPrefix(chunks[0], "\x1b_Ga=T,U=1,f=100,t=d,q=2,i=7,c=5,r=2,m=1;") {
			t.Errorf("first = %q", chunks[0][:40])
		}
		if !strings.HasPrefix(chunks[1], "\x1b_Gm=1;") || !strings.HasPrefix(chunks[2], "\x1b_Gm=0;") {
			t.Errorf("continuations = %q / %q", chunks[1][:8], chunks[2][:8])
		}
	})

	t.Run("RD-05 inside tmux the transmission is wrapped for passthrough", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")

		text := kittyTransmission("QUJD", 1, Box{Cols: 1, Rows: 1})

		if !strings.HasPrefix(text, "\x1bPtmux;\x1b\x1b_G") || !strings.HasSuffix(text, "\x1b\x1b\\\x1b\\") {
			t.Errorf("text = %q", text)
		}
	})
}

func TestHalfBlocksAndFit(t *testing.T) {
	t.Run("RD-06 half blocks give one text row per two pixel rows, width as the box", func(t *testing.T) {
		text := halfBlocks(solid(10, 10, color.RGBA{255, 0, 0, 255}), Box{Cols: 6, Rows: 3})

		lines := strings.Split(text, "\n")
		if len(lines) != 3 || ansi.StringWidth(lines[0]) != 6 || !strings.Contains(lines[0], "38;2;255;0;0") {
			t.Errorf("lines = %q", lines)
		}
	})

	t.Run("RD-07 fit keeps proportions, the column limit and never enlarges", func(t *testing.T) {
		wide := fit(solid(1000, 100, color.Black), 50, 10, 20)
		small := fit(solid(30, 40, color.Black), 50, 10, 20)

		if wide.Cols != 50 || wide.Rows != 2 {
			t.Errorf("wide = %+v", wide)
		}
		if small.Cols != 3 || small.Rows != 2 {
			t.Errorf("small = %+v", small)
		}
	})
}

func TestDetect(t *testing.T) {
	t.Run("RD-08 Detect picks kitty for kitty/ghostty, blocks for truecolor, else off", func(t *testing.T) {
		clear := func() {
			for _, name := range []string{"KITTY_WINDOW_ID", "TERM", "TERM_PROGRAM", "GHOSTTY_RESOURCES_DIR", "COLORTERM"} {
				t.Setenv(name, "")
			}
		}
		clear()
		t.Setenv("TERM_PROGRAM", "ghostty")
		ghostty := Detect(ModeAuto)
		clear()
		t.Setenv("COLORTERM", "truecolor")
		truecolor := Detect(ModeAuto)
		clear()
		plain := Detect(ModeAuto)

		if ghostty != ModeKitty || truecolor != ModeBlocks || plain != ModeOff || Detect(ModeBlocks) != ModeBlocks {
			t.Errorf("ghostty = %s truecolor = %s plain = %s", ghostty, truecolor, plain)
		}
	})
}

func TestMarkersAndMermaid(t *testing.T) {
	t.Run("RD-09 a saved image is marked, found again, and has a usable extension", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		imageDir = sync.OnceValues(newImageDir)

		marker, err := SaveImage([]byte("jpeg bytes"), "image/jpeg")
		paths := MarkedImages("before " + marker + " after")

		if err != nil || len(paths) != 1 || filepath.Ext(paths[0]) != ".jpg" || !IsImageFile(paths[0]) {
			t.Fatalf("marker = %q paths = %v err = %v", marker, paths, err)
		}
		if data, _ := os.ReadFile(paths[0]); string(data) != "jpeg bytes" {
			t.Errorf("data = %q", data)
		}
	})

	t.Run("RD-10 Mermaid draws with box characters; what it cannot draw stays source", func(t *testing.T) {
		renderer := Renderer{Mode: ModeOff, Width: 80}

		drawn := renderer.Mermaid("graph LR\n  A --> B\n")
		unknown := renderer.Mermaid("pie title Pets\n  \"Dogs\" : 3\n")

		if !strings.Contains(drawn, "─") || !strings.Contains(drawn, "A") {
			t.Errorf("drawn = %q", drawn)
		}
		if !strings.HasPrefix(unknown, "```mermaid\npie") {
			t.Errorf("unknown = %q", unknown)
		}
	})

	t.Run("RD-11 SVG text is drawn with an installed font, also for generic families", func(t *testing.T) {
		if found, err := engine(); err != nil || found.family == "" {
			t.Skip("no usable font on this machine")
		}
		svg := `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60"><rect width="200" height="60" fill="#000"/>` +
			`<text x="10" y="45" font-size="40" fill="#fff" font-family="sans-serif">mo</text></svg>`

		img, err := rasterizeSVG([]byte(svg), 400, 120)

		if err != nil || whitePixels(img) == 0 {
			t.Errorf("err = %v white pixels = %d", err, whitePixels(img))
		}
	})

	t.Run("RD-12 with images off an image is named, not drawn", func(t *testing.T) {
		var encoded bytes.Buffer
		png.Encode(&encoded, solid(12, 7, color.White))

		text, err := Renderer{Mode: ModeOff}.ImageData(encoded.Bytes())

		if err != nil || text != "[image 12x7]" || !utf8.ValidString(text) {
			t.Errorf("text = %q err = %v", text, err)
		}
	})
}

func whitePixels(img image.Image) int {
	if img == nil {
		return 0
	}
	count := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 0xf000 && g > 0xf000 && b > 0xf000 {
				count++
			}
		}
	}
	return count
}

func TestPhaseFiveFixes(t *testing.T) {
	t.Run("RD-13 inside another code block, image lines and mermaid fences stay text", func(t *testing.T) {
		markdown := "Add this:\n````markdown\n# Title\n![logo](docs/logo.png)\n```mermaid\ngraph LR\n```\n````\nDone\n"

		segments := Split(markdown)

		if len(segments) != 1 || segments[0].Kind != SegmentText || segments[0].Content != markdown {
			t.Errorf("segments = %+v", segments)
		}
	})

	t.Run("RD-14 an image whose header declares too many pixels is refused undecoded", func(t *testing.T) {
		var header bytes.Buffer
		png.Encode(&header, solid(1, 1, color.White))
		bomb := header.Bytes()
		binaryWidth := []byte{0, 0, 0xea, 0x60}
		copy(bomb[16:20], binaryWidth)
		copy(bomb[20:24], binaryWidth)
		binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))

		_, err := Decode(bomb)

		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("RD-15 malformed SVG is refused before the renderer sees it", func(t *testing.T) {
		for _, svg := range []string{"<svg><rect></svg>", "<html></html>", "not xml"} {
			if _, err := (Renderer{Mode: ModeBlocks, Width: 40}).SVG([]byte(svg)); err == nil {
				t.Errorf("%q was accepted", svg)
			}
		}
	})

	t.Run("RD-16 text is drawn for quoted, single-quoted, style and CSS font families", func(t *testing.T) {
		if found, err := engine(); err != nil || found.family == "" {
			t.Skip("no usable font on this machine")
		}
		for _, text := range []string{
			`<text x="10" y="45" font-size="40" fill="#fff" font-family='Arial'>mo</text>`,
			`<style>text{font-family:"trebuchet ms",verdana,sans-serif;}</style><text x="10" y="45" font-size="40" fill="#fff">mo</text>`,
			`<text x="10" y="45" style="font-size:40px;fill:#fff;font-family:Arial">mo</text>`,
		} {
			svg := `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60"><rect width="200" height="60" fill="#000"/>` + text + `</svg>`
			img, err := rasterizeSVG([]byte(svg), 400, 120)
			if err != nil || whitePixels(img) == 0 {
				t.Errorf("%s: err = %v white = %d", text, err, whitePixels(img))
			}
		}
	})

	t.Run("RD-17 a huge SVG is drawn straight at the size it is shown", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" width="100000" height="50000"><rect width="100" height="100" fill="red"/></svg>`

		img, err := rasterizeSVG([]byte(svg), 640, 640)

		if err != nil || img.Bounds().Dx() != 640 || img.Bounds().Dy() != 320 {
			t.Errorf("bounds = %v err = %v", img, err)
		}
	})

	t.Run("RD-18 kitty sends the image at its size on screen, not its full resolution", func(t *testing.T) {
		t.Setenv("TMUX", "")
		big := solid(2000, 1000, color.RGBA{10, 20, 30, 255})

		text, err := kittyImage(big, Box{Cols: 20, Rows: 5}, 10, 20)

		if err != nil || len(text) > 20_000 {
			t.Errorf("sent %d bytes, err = %v", len(text), err)
		}
	})

	t.Run("RD-19 saved images live in a private directory removed at the end", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		imageDir = sync.OnceValues(newImageDir)
		marker, err := SaveImage([]byte("data"), "image/png")
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(MarkedImages(marker)[0])
		info, _ := os.Stat(dir)

		RemoveSavedImages()

		if info == nil || info.Mode().Perm() != 0o700 || !strings.HasPrefix(filepath.Base(dir), "mo-images-") {
			t.Errorf("dir = %s mode = %v", dir, info.Mode())
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still there", dir)
		}
	})

	t.Run("RD-20 inside tmux without passthrough, kitty falls back to half blocks", func(t *testing.T) {
		t.Setenv("KITTY_WINDOW_ID", "1")
		t.Setenv("TMUX", "/nonexistent/socket,1,0")
		t.Setenv("PATH", t.TempDir())

		if mode := Detect(ModeAuto); mode != ModeBlocks {
			t.Errorf("mode = %s", mode)
		}
	})
}

func TestSVGSizes(t *testing.T) {
	t.Run("RD-21 width 100% with a viewBox keeps the viewBox's proportions", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 800 200"><circle cx="100" cy="100" r="90" fill="red"/></svg>`

		img, err := rasterizeSVG([]byte(svg), 400, 400)

		if err != nil || img.Bounds().Dx() != 400 || img.Bounds().Dy() != 100 {
			t.Errorf("bounds = %v err = %v", img, err)
		}
	})

	t.Run("RD-22 an SVG without xmlns is drawn; one in another namespace is refused", func(t *testing.T) {
		plain, err := rasterizeSVG([]byte(`<svg width="100" height="50"><rect width="100" height="50" fill="red"/></svg>`), 200, 200)
		_, foreignErr := rasterizeSVG([]byte(`<svg xmlns="urn:other" width="10" height="10"/>`), 100, 100)

		if err != nil || plain.Bounds().Dx() != 200 || foreignErr == nil {
			t.Errorf("plain = %v err = %v foreign = %v", plain, err, foreignErr)
		}
	})

	t.Run("RD-23 absolute units convert to pixels; em alone falls back to a default", func(t *testing.T) {
		inches, inchesKnown := absoluteLength("2in")
		points, _ := absoluteLength("12pt")
		_, emKnown := absoluteLength("40em")
		width, height := svgSize(map[string]string{"width": "40em", "height": "20em"})

		if !inchesKnown || inches != 192 || points != 16 || emKnown || width != defaultSVGWidth || height != defaultSVGHeight {
			t.Errorf("in = %v pt = %v em known = %v default = %vx%v", inches, points, emKnown, width, height)
		}
	})
}
