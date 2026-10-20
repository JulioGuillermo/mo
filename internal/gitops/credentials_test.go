package gitops

// Test Traceability Table
// | ID    | Category | Description                                                          | Priority |
// | GC-01 | Happy    | The store helper's file gives the login of the remote's host          | P0       |
// | GC-02 | Happy    | Another helper is asked as git asks it, by its program, for the host  | P0       |
// | GC-03 | Edge     | A helper set for another URL is not asked; cache is left to git      | P1       |
// | GC-04 | Happy    | A helper in quotes, a path with spaces as Windows has, runs whole      | P1       |
// | GC-05 | Happy    | A helper written as a shell command, !..., runs in sh with get after it | P1      |

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitConfig(t *testing.T, content string) {
	t.Helper()
	home := os.Getenv("HOME")
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialHelpers(t *testing.T) {
	t.Run("GC-01 the store helper's file gives the login of the remote's host", func(t *testing.T) {
		tr := newRepo(t)
		store := filepath.Join(t.TempDir(), "credentials")
		_ = os.WriteFile(store, []byte("https://alice:wrong@other.example\nhttps://bob:s3cret@git.example\n"), 0o600)
		gitConfig(t, "[credential]\n\thelper = store --file="+store+"\n")

		login := tr.r.helperAuth("https://git.example/team/repo.git")

		if login == nil || login.Username != "bob" || login.Password != "s3cret" {
			t.Errorf("login = %+v", login)
		}
	})

	t.Run("GC-02 another helper is asked as git asks it, by its program, for the host", func(t *testing.T) {
		tr := newRepo(t)
		bin := t.TempDir()
		asked := filepath.Join(bin, "asked")
		script := "#!/bin/sh\ncat > " + asked + "\necho username=carol\necho password=t0ken\n"
		if err := os.WriteFile(filepath.Join(bin, "git-credential-fake"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		gitConfig(t, "[credential]\n\thelper = fake\n")

		login := tr.r.helperAuth("https://code.example/x.git")

		request, _ := os.ReadFile(asked)
		if login == nil || login.Username != "carol" || login.Password != "t0ken" || !strings.Contains(string(request), "protocol=https\nhost=code.example\n") {
			t.Errorf("login = %+v request = %q", login, request)
		}
	})

	t.Run("GC-03 a helper set for another URL is not asked; cache is left to git", func(t *testing.T) {
		tr := newRepo(t)
		store := filepath.Join(t.TempDir(), "credentials")
		_ = os.WriteFile(store, []byte("https://dave:pw@git.example\n"), 0o600)
		gitConfig(t, "[credential \"https://elsewhere.example\"]\n\thelper = store --file="+store+"\n[credential]\n\thelper = cache\n")

		if login := tr.r.helperAuth("https://git.example/x.git"); login != nil {
			t.Errorf("login = %+v", login)
		}
	})

	t.Run("GC-04 a helper in quotes, a path with spaces as Windows has, runs whole", func(t *testing.T) {
		tr := newRepo(t)
		dir := filepath.Join(t.TempDir(), "Program Files", "Git")
		_ = os.MkdirAll(dir, 0o755)
		helper := filepath.Join(dir, "credential manager")
		if err := os.WriteFile(helper, []byte("#!/bin/sh\n[ \"$1 $2\" = \"--x get\" ] && echo password=quoted\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		gitConfig(t, "[credential]\n\thelper = \"\\\""+helper+"\\\" --x\"\n")

		login := tr.r.helperAuth("https://code.example/x.git")
		name, options := helperName(`"C:/Program Files/Git/gcm.exe" --flag`)

		if login == nil || login.Password != "quoted" || name != "C:/Program Files/Git/gcm.exe" || options != "--flag" {
			t.Errorf("login = %+v name %q options %q", login, name, options)
		}
	})

	t.Run("GC-05 a helper written as a shell command, !..., runs in sh with get after it", func(t *testing.T) {
		tr := newRepo(t)
		gitConfig(t, "[credential]\n\thelper = \"!f() { [ \\\"$1\\\" = get ] && echo username=eve && echo password=sh; }; f\"\n")

		login := tr.r.helperAuth("https://code.example/x.git")

		if login == nil || login.Username != "eve" || login.Password != "sh" {
			t.Errorf("login = %+v", login)
		}
	})
}
