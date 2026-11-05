package tools

// Test Traceability Table
// | ID    | Category | Description                                                              | Priority |
// | PD-01 | Happy    | A PDF reads as text by default, a section per page                       | P0       |
// | PD-02 | Happy    | pages limits the text to a page or a range                               | P0       |
// | PD-03 | Happy    | as image gives a PNG marker per page, drawn once and kept in the cache   | P0       |
// | PD-04 | Error    | More than 20 pages as images is refused, saying how to ask for fewer     | P1       |
// | PD-05 | Error    | pages that are not a page or a range, or past the end, are refused       | P1       |
// | PD-06 | Error    | as and pages on a file that is not a PDF are refused                     | P2       |
// | PD-07 | Error    | A file named .pdf that is not one is refused, saying so                  | P1       |
// | PD-08 | Happy    | The view fields apply to the text of a PDF                               | P2       |

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jg/mo/internal/render"
)

// minimalPDF is a PDF of the given pages, each one line of text.
func minimalPDF(pageTexts ...string) string {
	count := len(pageTexts)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var kids []string
	for i, text := range pageTexts {
		pageNumber, contentNumber := 4+2*i, 5+2*i
		kids = append(kids, fmt.Sprintf("%d 0 R", pageNumber))
		stream := fmt.Sprintf("BT /F1 24 Tf 72 720 Td (%s) Tj ET", text)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", contentNumber),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), count)

	var pdf strings.Builder
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return pdf.String()
}

func TestReadPDF(t *testing.T) {
	t.Run("PD-01 a PDF reads as text by default, a section per page", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "doc.pdf", minimalPDF("first words", "second words"))

		output, isError := call(t, env, "read", map[string]any{"path": "doc.pdf"})

		if isError || !strings.Contains(output, "## Page 1\n\nfirst words") || !strings.Contains(output, "## Page 2\n\nsecond words") {
			t.Fatalf("output %q", output)
		}
	})

	t.Run("PD-02 pages limits the text to a page or a range", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "doc.pdf", minimalPDF("one", "two", "three"))

		single, _ := call(t, env, "read", map[string]any{"path": "doc.pdf", "pages": "2"})
		tail, _ := call(t, env, "read", map[string]any{"path": "doc.pdf", "pages": "2-9"})

		if strings.Contains(single, "one") || !strings.Contains(single, "## Page 2\n\ntwo") || strings.Contains(single, "three") {
			t.Fatalf("page 2: %q", single)
		}
		if strings.Contains(tail, "## Page 1") || !strings.Contains(tail, "## Page 3\n\nthree") {
			t.Fatalf("pages 2-9: %q", tail)
		}
	})

	t.Run("PD-03 as image gives a PNG marker per page, drawn once and kept in the cache", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		env := newEnv(t)
		writeFile(t, env, "doc.pdf", minimalPDF("one", "two"))

		output, isError := call(t, env, "read", map[string]any{"path": "doc.pdf", "as": "image"})

		images := render.MarkedImages(output)
		if isError || len(images) != 2 || !strings.Contains(output, "page 2 of 2: [image png 850x1100") {
			t.Fatalf("output %q", output)
		}
		if err := os.WriteFile(images[0], []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
		again, _ := call(t, env, "read", map[string]any{"path": "doc.pdf", "as": "image", "pages": "1"})
		if !strings.Contains(again, "unreadable image") {
			t.Fatalf("the page was drawn again instead of taken from the cache: %q", again)
		}
	})

	t.Run("PD-04 more than 20 pages as images is refused, saying how to ask for fewer", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "long.pdf", minimalPDF(strings.Split(strings.Repeat("p,", 21)+"p", ",")...))

		output, isError := call(t, env, "read", map[string]any{"path": "long.pdf", "as": "image"})

		if !isError || !strings.Contains(output, "22 pages") || !strings.Contains(output, "1-20") {
			t.Fatalf("isError %v, output %q", isError, output)
		}
	})

	t.Run("PD-05 pages that are not a page or a range, or past the end, are refused", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "doc.pdf", minimalPDF("one", "two"))

		for pages, want := range map[string]string{"x": "not \"x\"", "3-1": "not \"3-1\"", "0": "not \"0\"", "5": "has 2 pages"} {
			output, isError := call(t, env, "read", map[string]any{"path": "doc.pdf", "pages": pages})
			if !isError || !strings.Contains(output, want) {
				t.Errorf("pages %q: isError %v, output %q", pages, isError, output)
			}
		}
	})

	t.Run("PD-06 as and pages on a file that is not a PDF are refused", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "notes.txt", "text")

		output, isError := call(t, env, "read", map[string]any{"path": "notes.txt", "as": "image"})

		if !isError || !strings.Contains(output, "for a PDF") {
			t.Fatalf("isError %v, output %q", isError, output)
		}
	})

	t.Run("PD-07 a file named .pdf that is not one is refused, saying so", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "fake.pdf", "just some text")

		output, isError := call(t, env, "read", map[string]any{"path": "fake.pdf"})

		if !isError || !strings.Contains(output, "not a PDF") {
			t.Fatalf("isError %v, output %q", isError, output)
		}
	})

	t.Run("PD-08 the view fields apply to the text of a PDF", func(t *testing.T) {
		env := newEnv(t)
		writeFile(t, env, "doc.pdf", minimalPDF("one", "two"))

		output, isError := call(t, env, "read", map[string]any{"path": "doc.pdf", "match": "two"})

		if isError || strings.Contains(output, "one") || !strings.Contains(output, "two") {
			t.Fatalf("output %q", output)
		}
	})
}
