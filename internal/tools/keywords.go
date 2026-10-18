package tools

import "slices"

// Keywords help tool_search and tool_list find a built-in tool by the words
// a model may use for what it wants: synonyms, and the topics tools share, so
// a search for "file" finds those that work on files. They are for the
// search only: the model never receives them, so they cost no tokens, and a
// description changed in the config is found by them all the same.
var topics = map[string][]string{
	"files":   {"file", "files", "filesystem", "path", "disk", "document", "archivo", "archivos", "fichero"},
	"folders": {"folder", "folders", "directory", "directories", "dir", "tree", "carpeta", "directorio"},
	"change":  {"change", "modify", "alter", "update", "cambiar", "modificar"},
	"web":     {"internet", "web", "online", "http", "https", "url", "site", "website", "page", "link", "browser", "network", "navegador", "red"},
	"find":    {"find", "search", "lookup", "locate", "query", "look", "buscar", "encontrar"},
	"shell":   {"shell", "terminal", "command", "commands", "cmd", "script", "execute", "process", "program", "comando", "ejecutar"},
}

func topic(names ...string) []string {
	var words []string
	for _, name := range names {
		words = append(words, topics[name]...)
	}
	return words
}

// builtinKeywords are the words each built-in tool is found by, besides its
// name and its description.
var builtinKeywords = map[string][]string{
	"bash": slices.Concat(topic("shell"), []string{"run", "run command", "script", "background", "job", "build", "compile", "make", "test",
		"install", "kill", "output", "stdout", "pipe", "sh", "zsh", "linux", "ssh", "docker", "npm", "go test", "ejecutar", "correr", "compilar", "proceso"}),
	"read": slices.Concat(topic("files"), []string{"open", "view", "show", "cat", "print", "display", "contents", "content", "load", "image", "picture",
		"photo", "screenshot", "png", "jpg", "pdf", "leer", "abrir", "ver", "mostrar", "imagen"}),
	"write": slices.Concat(topic("files", "change"), []string{"create", "new", "save", "overwrite", "output", "generate", "put", "dump", "store", "touch",
		"escribir", "crear", "guardar", "generar", "sobrescribir"}),
	"edit": slices.Concat(topic("files", "change"), []string{"replace", "substitute", "patch", "rewrite", "fix", "refactor", "rename", "sed", "swap",
		"correct", "editar", "reemplazar", "sustituir", "corregir", "arreglar"}),
	"ls":    slices.Concat(topic("files", "folders"), []string{"list", "show", "contents", "browse", "dir", "tree", "find files", "names", "listar", "mostrar", "ver"}),
	"mkdir": slices.Concat(topic("folders"), []string{"create", "make", "new", "mkdir", "crear", "nueva"}),
	"rm": slices.Concat(topic("files", "folders"), []string{"delete", "remove", "erase", "unlink", "clean", "destroy", "trash", "drop", "wipe",
		"borrar", "eliminar", "quitar", "limpiar"}),
	"grep": slices.Concat(topic("files", "find"), []string{"regex", "regexp", "pattern", "match", "text", "contents", "occurrences", "usages", "references",
		"string", "ripgrep", "rg", "scan", "buscar texto", "patron", "coincidencias"}),
	"web_fetch": slices.Concat(topic("web"), []string{"fetch", "download", "get", "html", "markdown", "read page", "scrape", "open url", "curl", "wget", "request", "api",
		"descargar", "leer web", "enlace"}),
	"web_search": slices.Concat(topic("web", "find"), []string{"google", "bing", "duckduckgo", "news", "documentation", "docs", "answer", "latest", "results",
		"snippets", "buscar en internet", "noticias", "documentacion"}),
	"memory": {"remember", "memory", "memories", "notes", "note", "save", "recall", "persist", "history", "past", "conversation", "conversations", "previous",
		"preference", "preferences", "knowledge", "forget", "memoria", "recordar", "nota", "notas", "apuntar", "olvidar"},
	"todo": {"todo", "plan", "checklist", "tasks", "task", "steps", "progress", "list of tasks", "track", "organize", "status", "pending", "done",
		"tarea", "tareas", "lista", "pasos", "progreso"},
	"ask": {"ask", "question", "questions", "user", "confirm", "choice", "choose", "answer", "clarify", "decision", "options", "poll", "prompt",
		"pregunta", "preguntar", "usuario", "confirmar", "elegir", "decision"},
	"git": {"git", "repository", "repo", "version", "versioning", "commit", "branch", "diff", "status", "log", "blame", "merge", "rebase", "push", "pull",
		"stash", "tag", "checkout", "switch", "history", "changes", "staged", "conflict", "conflicts", "control", "repositorio", "rama", "cambios"},
	"title": {"title", "name", "rename", "conversation", "label", "titulo", "nombre", "renombrar", "conversacion"},
	"ui": {"ui", "interface", "form", "dialog", "window", "panel", "picker", "input", "widget", "screen", "settings", "button", "menu", "table", "tool for the user",
		"formulario", "ventana", "interfaz", "pantalla", "boton"},
	AgentName: {"agent", "agents", "subagent", "subagents", "delegate", "parallel", "worker", "helper", "spawn", "task", "background", "research", "explore",
		"delegar", "agente", "subagente", "paralelo"},
	TellParentName: {"parent", "report", "finding", "tell", "message", "notify", "agent", "subagent", "inform", "padre", "avisar", "informar"},
}

// keywordsOf are the words a tool is found by besides its name and its description.
func keywordsOf(tool Tool) []string {
	return slices.Concat(tool.Keywords, builtinKeywords[tool.Name])
}
