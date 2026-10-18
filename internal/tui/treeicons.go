package tui

import (
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// The tree draws a folder and each kind of file with an icon, of the set that
// render.icons names: "nerd" Nerd Font's, each in the colour of its language;
// "emoji", for any terminal, which have their own; "text", none, a folder is
// ▸ or ▾ and ends with a /.
const iconsEmoji = "emoji"

// fileKind is the kind of a file by its name: the language, or the sort of
// thing it is; "file" when it is none of those known.
func fileKind(name string) string {
	lower := strings.ToLower(name)
	switch lower {
	case "dockerfile", "containerfile":
		return "docker"
	case "makefile", "gnumakefile", "justfile", "cmakelists.txt":
		return "config"
	case ".gitignore", ".gitattributes", ".gitmodules", ".gitkeep":
		return "git"
	case "go.sum", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "cargo.lock", "poetry.lock", "uv.lock", "gemfile.lock", "composer.lock":
		return "lock"
	}
	switch strings.TrimPrefix(filepath.Ext(lower), ".") {
	case "go":
		return "go"
	case "py", "pyw", "pyi":
		return "python"
	case "js", "mjs", "cjs", "jsx":
		return "javascript"
	case "ts", "tsx", "mts", "cts":
		return "typescript"
	case "rs":
		return "rust"
	case "c", "h":
		return "c"
	case "cc", "cpp", "cxx", "hpp", "hh", "hxx":
		return "cpp"
	case "java":
		return "java"
	case "lua":
		return "lua"
	case "sh", "bash", "zsh", "fish":
		return "shell"
	case "md", "markdown", "mdx":
		return "markdown"
	case "json", "jsonc", "json5":
		return "json"
	case "yml", "yaml":
		return "yaml"
	case "toml", "ini", "conf", "cfg", "env", "properties":
		return "config"
	case "html", "htm":
		return "html"
	case "css", "scss", "sass", "less":
		return "css"
	case "sql":
		return "sql"
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif":
		return "image"
	case "pdf":
		return "pdf"
	case "zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar", "zst":
		return "archive"
	case "lock":
		return "lock"
	case "txt", "log", "text":
		return "text"
	}
	return "file"
}

// treeIcon is an icon and, for Nerd Font's, its colour.
type treeIcon struct {
	glyph string
	color color.Color
}

func hue(hex string) color.Color { return lipgloss.Color(hex) }

// nerdIcons are Nerd Font's icons by kind: Font Awesome's for folders and
// generic files, Seti and Devicons for languages.
var nerdIcons = map[string]treeIcon{
	"folder":     {"", hue("#7aa2f7")},
	"folderOpen": {"", hue("#7aa2f7")},
	"file":       {"", hue("#737aa2")},
	"text":       {"", hue("#737aa2")},
	"go":         {"", hue("#00add8")},
	"python":     {"", hue("#ffbc03")},
	"javascript": {"", hue("#f7df1e")},
	"typescript": {"", hue("#3178c6")},
	"rust":       {"", hue("#dea584")},
	"c":          {"", hue("#599eff")},
	"cpp":        {"", hue("#599eff")},
	"java":       {"", hue("#cc3e44")},
	"lua":        {"", hue("#51a0cf")},
	"shell":      {"", hue("#9ece6a")},
	"markdown":   {"", hue("#7dcfff")},
	"json":       {"", hue("#e0af68")},
	"yaml":       {"", hue("#bb9af7")},
	"config":     {"", hue("#737aa2")},
	"html":       {"", hue("#e44d26")},
	"css":        {"", hue("#42a5f5")},
	"docker":     {"", hue("#2496ed")},
	"git":        {"", hue("#f1502f")},
	"sql":        {"", hue("#737aa2")},
	"image":      {"", hue("#bb9af7")},
	"pdf":        {"", hue("#f7768e")},
	"archive":    {"", hue("#e0af68")},
	"lock":       {"", hue("#737aa2")},
}

// emojiIcons are the same by kind, as emoji, which have colours of their own.
var emojiIcons = map[string]string{
	"folder": "📁", "folderOpen": "📂", "file": "📄", "text": "📝",
	"go": "🐹", "python": "🐍", "javascript": "🟨", "typescript": "🟦", "rust": "🦀", "c": "🔵", "cpp": "🔷",
	"java": "☕", "lua": "🌙", "shell": "🐚", "markdown": "📘", "json": "🧾", "yaml": "🧩", "config": "🔧",
	"html": "🌐", "css": "🎨", "docker": "🐳", "git": "🌿", "sql": "💾", "image": "🌄", "pdf": "📕",
	"archive": "📦", "lock": "🔒",
}

// iconOf is the icon of a folder, open or not, or of a file by its name, in the
// set asked for: none in text.
func iconOf(set, name string, dir, open bool) (treeIcon, bool) {
	kind := fileKind(name)
	switch {
	case dir && open:
		kind = "folderOpen"
	case dir:
		kind = "folder"
	}
	switch set {
	case iconsNerd:
		icon, found := nerdIcons[kind]
		return icon, found
	case iconsEmoji:
		glyph, found := emojiIcons[kind]
		return treeIcon{glyph: glyph}, found
	}
	return treeIcon{}, false
}

// treeIconSet is the icons the tree draws: what render.icons says.
func (m model) treeIconSet() string {
	switch m.session.Config.Render.Icons {
	case iconsText:
		return iconsText
	case iconsEmoji:
		return iconsEmoji
	}
	return iconsNerd
}
