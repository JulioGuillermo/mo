package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Files are the user file and the project file of a folder as read, and
// the settings they make.
type Files struct {
	User     Config
	Project  Project
	Settings Config
	contents map[string][]byte
}

// BrokenFile is a settings file that cannot be read, or that holds a value
// mo does not take.
type BrokenFile struct {
	Path string
	Err  error
}

func (b *BrokenFile) Error() string {
	return fmt.Sprintf("%s: %v", b.Path, b.Err)
}

func (b *BrokenFile) Unwrap() error {
	return b.Err
}

// ReadFiles reads the settings of workDir; when a file is wrong the error
// is a *BrokenFile naming it.
func ReadFiles(workDir string) (Files, error) {
	files := Files{contents: map[string][]byte{}}
	userData, err := readIfThere(UserFile())
	if err != nil {
		return files, &BrokenFile{Path: UserFile(), Err: err}
	}
	if _, err := toml.Decode(string(userData), &files.User); err != nil {
		return files, &BrokenFile{Path: UserFile(), Err: err}
	}
	files.contents[UserFile()] = userData
	if _, err := combine(workDir, files.User, Project{}); err != nil {
		return files, &BrokenFile{Path: UserFile(), Err: err}
	}

	files.Project, err = ReadProject(workDir)
	if err != nil {
		return files, &BrokenFile{Path: orDefault(files.Project.Path, ProjectFile(workDir)), Err: errors.Unwrap(err)}
	}
	if files.Project.Path != "" {
		files.contents[files.Project.Path] = files.Project.data
	}
	files.Settings, err = combine(workDir, files.User, files.Project)
	if err != nil {
		return files, &BrokenFile{Path: files.Project.Path, Err: err}
	}
	return files, nil
}

// Restore puts back a file as it was when these files were read, keeping
// the one on disk next to it as path.broken.
func (f Files) Restore(path string) error {
	if err := keepBroken(path); err != nil {
		return err
	}
	data, existed := f.contents[path]
	if !existed {
		return nil
	}
	permissions := os.FileMode(0o644)
	if path == UserFile() {
		permissions = 0o600
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, permissions); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// ResetFile moves a settings file aside as path.broken, so mo goes back
// to the defaults for what it set.
func ResetFile(path string) error {
	return keepBroken(path)
}

func keepBroken(path string) error {
	err := os.Rename(path, path+".broken")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func readIfThere(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
