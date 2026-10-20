package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/render"
)

const (
	largestImage    = 8 << 20
	moNotePrefix    = "[mo] "
	attachedNote    = moNotePrefix + "The user attached these files; read them with the read tool when you need them:"
	toolImagesNote  = moNotePrefix + "The images the tool calls above returned, in order: %s"
	noVisionNote    = "[image not sent: this model does not take images]"
	attachedSummary = "attached: %s"
)

var imageMIME = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

// ownText is what the user wrote of a message: its text before mo's first
// note, none when it is a note. Files are looked for only there, never in
// what mo added, as a command's output.
func ownText(text string) string {
	if strings.HasPrefix(text, moNotePrefix) {
		return ""
	}
	own, _, _ := strings.Cut(text, "\n\n"+moNotePrefix)
	return own
}

// promptMessage is the user's prompt with the files it names, as pasted or
// dropped paths: images go with it, other files are listed for the model to
// read.
func (a *Agent) promptMessage(prompt string, hooks Hooks) llm.Message {
	return a.promptMessageNaming(prompt, ownText(prompt), hooks)
}

// promptMessageNaming is promptMessage with the files of own, the text the
// user wrote.
func (a *Agent) promptMessageNaming(prompt, own string, hooks Hooks) llm.Message {
	message := llm.Message{Role: llm.RoleUser, Text: prompt}
	var others, names []string
	for _, path := range namedFiles(own, a.Tools.WorkDir()) {
		names = append(names, filepath.Base(path))
		if image, err := loadImage(path); err == nil {
			message.Images = append(message.Images, image)
			continue
		}
		others = append(others, path)
	}
	if len(others) > 0 {
		message.Text += "\n\n" + attachedNote + "\n" + strings.Join(others, "\n")
	}
	if len(names) > 0 && hooks.OnNotice != nil {
		hooks.OnNotice(fmt.Sprintf(attachedSummary, strings.Join(names, ", ")))
	}
	return message
}

// toolImages loads the images that read and MCP tools marked in their
// results, as a note for the model to see them.
func toolImages(calls []llm.ToolCall, results []llm.Message) (llm.Message, bool) {
	note := llm.Message{Role: llm.RoleUser}
	var names []string
	for i, call := range calls {
		if call.Name != "read" && !strings.HasPrefix(call.Name, "mcp__") {
			continue
		}
		for _, path := range render.MarkedImages(results[i].Text) {
			if image, err := loadImage(path); err == nil {
				note.Images = append(note.Images, image)
				names = append(names, filepath.Base(path))
			}
		}
	}
	note.Text = fmt.Sprintf(toolImagesNote, strings.Join(names, ", "))
	return note, len(note.Images) > 0
}

func loadImage(path string) (llm.Image, error) {
	mime, isImage := imageMIME[strings.ToLower(filepath.Ext(path))]
	if !isImage {
		return llm.Image{}, fmt.Errorf("%s is not a picture the model takes", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return llm.Image{}, err
	}
	if info.Size() > largestImage {
		return llm.Image{}, fmt.Errorf("%s is larger than %d MB", path, largestImage>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return llm.Image{}, err
	}
	return llm.Image{MIME: mime, Data: data}, nil
}

// withoutImages replaces every image in the history with a line saying it
// was not sent, and tells whether there was any.
func (a *Agent) withoutImages() bool {
	found := false
	for i := range a.History {
		if len(a.History[i].Images) == 0 {
			continue
		}
		found = true
		typed := PromptText(a.History[i])
		a.History[i].Images = nil
		a.History[i].Text += "\n" + noVisionNote
		if isPrompt(a.History[i]) {
			a.History[i] = withInput(a.History[i], typed)
		}
	}
	return found
}

// namedFiles returns the existing files the text names: absolute, home or
// relative paths, quoted or with escaped spaces, as terminals paste them.
func namedFiles(text, workDir string) []string {
	var files []string
	seen := map[string]bool{}
	for _, word := range shellWords(text) {
		word = strings.TrimPrefix(word, "file://")
		if !strings.ContainsAny(word, "/.") {
			continue
		}
		path := word
		if rest, home := strings.CutPrefix(word, "~/"); home {
			dir, _ := os.UserHomeDir()
			path = filepath.Join(dir, rest)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	return files
}

// shellWords splits text at spaces, keeping quoted parts and escaped spaces
// inside one word.
func shellWords(text string) []string {
	var words []string
	var word strings.Builder
	quote := rune(0)
	escaped := false
	for _, character := range text {
		switch {
		case escaped:
			word.WriteRune(character)
			escaped = false
		case character == '\\' && quote != '\'':
			escaped = true
		case quote != 0 && character == quote:
			quote = 0
		case quote == 0 && (character == '\'' || character == '"'):
			quote = character
		case quote == 0 && unicode.IsSpace(character):
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}
		default:
			word.WriteRune(character)
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
}

// PastedFiles returns the files a pasted text names when it is nothing but
// paths to existing files, as a terminal pastes a dropped file.
func PastedFiles(text, workDir string) []string {
	files := namedFiles(text, workDir)
	if len(files) == 0 || len(files) != len(shellWords(text)) {
		return nil
	}
	return files
}
