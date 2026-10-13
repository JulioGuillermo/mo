package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GM-01 | Happy    | Changes to different parts merge cleanly                              | P0       |
// | GM-02 | Happy    | The same change on both sides is taken once                          | P1       |
// | GM-03 | Happy    | Different changes to the same lines conflict, with ours, base and theirs | P0    |
// | GM-04 | Edge     | Lines added at the end on one side, removed on the other, no final newline | P1  |
// | GM-05 | Happy    | Markers finds conflicts and not a markdown heading's underline        | P0       |
// | GD-01 | Happy    | A unified diff shows changes with three lines of context, hunks apart | P1       |
// | GD-02 | Edge     | Equal texts give no diff; a missing final newline is said             | P2       |

import (
	"strings"
	"testing"
)

func TestMerge3(t *testing.T) {
	base := "a\nb\nc\nd\ne\nf\ng\n"

	t.Run("GM-01 changes to different parts merge cleanly", func(t *testing.T) {
		merged, conflicted := Merge3(base, "A\nb\nc\nd\ne\nf\ng\n", "a\nb\nc\nd\ne\nf\nG\n", "HEAD", "base", "feature")

		if conflicted || merged != "A\nb\nc\nd\ne\nf\nG\n" {
			t.Errorf("merged = %q conflicted = %v", merged, conflicted)
		}
	})

	t.Run("GM-02 the same change on both sides is taken once", func(t *testing.T) {
		changed := "a\nb\nC\nd\ne\nf\ng\n"
		merged, conflicted := Merge3(base, changed, changed, "HEAD", "base", "feature")

		if conflicted || merged != changed {
			t.Errorf("merged = %q", merged)
		}
	})

	t.Run("GM-03 different changes to the same lines conflict, with ours, base and theirs", func(t *testing.T) {
		merged, conflicted := Merge3(base, "a\nb\nOURS\nd\ne\nf\ng\n", "a\nb\nTHEIRS\nd\ne\nf\nG\n", "HEAD", "base", "feature")

		want := "a\nb\n<<<<<<< HEAD\nOURS\n||||||| base\nc\n=======\nTHEIRS\n>>>>>>> feature\nd\ne\nf\nG\n"
		if !conflicted || merged != want {
			t.Errorf("merged =\n%s", merged)
		}
	})

	t.Run("GM-04 lines added at the end on one side, removed on the other, no final newline", func(t *testing.T) {
		added, _ := Merge3("x\ny", "x\ny\nz", "x\ny", "HEAD", "base", "feature")
		removed, conflicted := Merge3("x\ny\nz\n", "x\nz\n", "x\ny\nz\nw\n", "HEAD", "base", "feature")

		if added != "x\ny\nz" || conflicted || removed != "x\nz\nw\n" {
			t.Errorf("added = %q removed = %q conflicted = %v", added, removed, conflicted)
		}
	})

	t.Run("GM-05 Markers finds conflicts and not a markdown heading's underline", func(t *testing.T) {
		merged, _ := Merge3(base, "a\nb\nOURS\nd\ne\nf\ng\n", "a\nb\nTHEIRS\nd\ne\nf\ng\n", "HEAD", "base", "feature")
		heading := "Title\n=======\n\ntext\n"

		if found := Markers(merged); len(found) != 1 || found[0].Line != 3 {
			t.Errorf("found = %+v", found)
		}
		if found := Markers(heading); len(found) != 0 {
			t.Errorf("a heading was taken for a conflict: %+v", found)
		}
	})
}

func TestUnified(t *testing.T) {
	t.Run("GD-01 a unified diff shows changes with three lines of context, hunks apart", func(t *testing.T) {
		var before, after []string
		for i := range 20 {
			before = append(before, string(rune('a'+i)))
			after = append(after, string(rune('a'+i)))
		}
		after[1], after[17] = "B", "R"

		diff := Unified("f.txt", strings.Join(before, "\n")+"\n", strings.Join(after, "\n")+"\n")

		want := "--- a/f.txt\n+++ b/f.txt\n@@ -1,5 +1,5 @@\n a\n-b\n+B\n c\n d\n e\n@@ -15,6 +15,6 @@\n o\n p\n q\n-r\n+R\n s\n t\n"
		if diff != want {
			t.Errorf("diff =\n%s\nwant\n%s", diff, want)
		}
	})

	t.Run("GD-02 equal texts give no diff; a missing final newline is said", func(t *testing.T) {
		same := Unified("f", "x\n", "x\n")
		noNewline := Unified("f", "x\n", "x\ny")

		if same != "" || !strings.Contains(noNewline, "+y\n\\ No newline at end of file") {
			t.Errorf("same = %q no newline = %q", same, noNewline)
		}
	})
}
