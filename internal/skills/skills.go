// Package skills finds the skills mo can give the model: folders with a
// SKILL.md whose front matter names the skill and says what it is for. The
// model loads one when a task calls for it, and reads the folder's other
// files from there.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// FileName is the file that makes a folder a skill.
const FileName = "SKILL.md"

type Skill struct {
	Name        string
	Description string
	// Dir is the skill's folder, where its other files are.
	Dir string
}

// Find lists the skills in the folders of dirs, each a folder of skill
// folders. A skill found again in a later folder replaces the earlier one,
// so the project's own win over the user's. Missing folders are skipped.
func Find(dirs []string) []Skill {
	byName := map[string]Skill{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			folder := filepath.Join(dir, entry.Name())
			if info, err := os.Stat(folder); err != nil || !info.IsDir() {
				continue
			}
			skill, err := Read(folder)
			if err != nil {
				continue
			}
			byName[skill.Name] = skill
		}
	}
	var found []Skill
	for _, skill := range byName {
		found = append(found, skill)
	}
	slices.SortFunc(found, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return found
}

// Without drops the skills named in off.
func Without(found []Skill, off []string) []Skill {
	var kept []Skill
	for _, skill := range found {
		if !slices.Contains(off, skill.Name) {
			kept = append(kept, skill)
		}
	}
	return kept
}

// Names are the names of the skills.
func Names(found []Skill) []string {
	var names []string
	for _, skill := range found {
		names = append(names, skill.Name)
	}
	return names
}

// Read reads the skill of a folder. Without a name in its front matter the
// folder names it; without a description, its first line of text does.
func Read(folder string) (Skill, error) {
	data, err := os.ReadFile(filepath.Join(folder, FileName))
	if err != nil {
		return Skill{}, err
	}
	fields, body := frontMatter(string(data))
	skill := Skill{Name: fields["name"], Description: fields["description"], Dir: folder}
	if skill.Name == "" {
		skill.Name = filepath.Base(folder)
	}
	if skill.Description == "" {
		skill.Description = firstLine(body)
	}
	return skill, nil
}

// Body is the text of a skill's SKILL.md after its front matter.
func (s Skill) Body() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, FileName))
	if err != nil {
		return "", err
	}
	_, body := frontMatter(string(data))
	return strings.TrimSpace(body), nil
}

// Load is what loading the skill gives the model: its instructions, its
// folder and up to filesShown of its other files.
func (s Skill) Load(filesShown int) (string, error) {
	body, err := s.Body()
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("Skill %s, in %s\n\n%s", s.Name, s.Dir, body)
	if files := s.Files(filesShown); len(files) > 0 {
		out += "\n\nIts other files, relative to its folder: " + strings.Join(files, ", ")
	}
	return out, nil
}

// Named is the skill of that name among found.
func Named(found []Skill, name string) (Skill, bool) {
	index := slices.IndexFunc(found, func(skill Skill) bool { return skill.Name == strings.TrimSpace(name) })
	if index < 0 {
		return Skill{}, false
	}
	return found[index], true
}

// Files lists the skill's other files, relative to its folder, up to limit.
func (s Skill) Files(limit int) []string {
	var files []string
	_ = filepath.WalkDir(s.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || len(files) >= limit {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			if path != s.Dir && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		relative, _ := filepath.Rel(s.Dir, path)
		if relative != FileName {
			files = append(files, relative)
		}
		return nil
	})
	return files
}

// frontMatter splits the YAML front matter between --- lines from the body
// and reads its top-level keys: plain or quoted values, and the indented
// lines of a block (| or >) or of a value that goes on below its key.
func frontMatter(text string) (map[string]string, string) {
	fields := map[string]string{}
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fields, text
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return fields, text
	}
	key := ""
	var value []string
	flush := func() {
		if key != "" {
			fields[key] = unquote(strings.Join(value, " "))
		}
	}
	for _, line := range lines[1:end] {
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if indented && key != "" {
			value = append(value, strings.TrimSpace(line))
			continue
		}
		name, rest, found := strings.Cut(line, ":")
		if !found || indented {
			continue
		}
		flush()
		key, value = strings.TrimSpace(name), nil
		if rest = strings.TrimSpace(rest); rest != "" && rest != "|" && rest != ">" && rest != "|-" && rest != ">-" {
			value = append(value, rest)
		}
	}
	flush()
	return fields, strings.Join(lines[end+1:], "\n")
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(strings.TrimLeft(line, "# ")); line != "" {
			return line
		}
	}
	return ""
}
