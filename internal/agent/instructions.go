package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Instruction is a file telling the agent how to work: the user's own, for
// every project, or one of the project's, such as AGENTS.md.
type Instruction struct {
	Path string
	Text string
}

// FindInstructions reads the files named in names from userDir, then from
// each folder between the repository's root and workDir, the root first, so
// the instructions closest to the work come last. Without a repository only
// workDir is read. Empty files are left out.
func FindInstructions(names []string, userDir, workDir string) []Instruction {
	dirs := []string{userDir}
	dirs = append(dirs, projectDirs(workDir)...)
	var found []Instruction
	var seen []string
	for _, dir := range dirs {
		for _, name := range names {
			path := filepath.Join(dir, name)
			real, err := filepath.EvalSymlinks(path)
			if dir == "" || err != nil || slices.Contains(seen, real) {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(data)) == "" {
				continue
			}
			seen = append(seen, real)
			found = append(found, Instruction{Path: path, Text: strings.TrimSpace(string(data))})
		}
	}
	return found
}

// projectDirs are the folders from the repository's root down to workDir.
func projectDirs(workDir string) []string {
	workDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil
	}
	dirs := []string{workDir}
	for dir := workDir; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			slices.Reverse(dirs)
			return dirs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return []string{workDir}
		}
		dir = parent
		dirs = append(dirs, dir)
	}
}

// instructionsText is what the found files add to the system prompt.
func instructionsText(found []Instruction) string {
	if len(found) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n\nInstructions for this work, from the user's and the project's files; follow them:")
	for _, instruction := range found {
		fmt.Fprintf(&out, "\n\n<instructions path=%q>\n%s\n</instructions>", instruction.Path, instruction.Text)
	}
	return out.String()
}
