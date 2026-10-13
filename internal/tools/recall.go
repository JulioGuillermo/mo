package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jg/mo/internal/llm"
	"github.com/jg/mo/internal/sessions"
)

const (
	recallName          = "recall"
	recallResults       = 6
	mostRecallResults   = 20
	snippetLength       = 220
	recalledToolInput   = 200
	recalledToolOutput  = 300
	readToolOutput      = 1500
	bm25K1              = 1.2
	bm25B               = 0.75
	scopeHere           = "here"
	scopeAll            = "all"
	kindMemories        = "memories"
	kindConversations   = "conversations"
	memoryPrefix        = "m:"
	conversationPrefix  = "c:"
	turnSeparator       = "#"
	moNotePrefix        = "[mo] "
	summaryHeaderPrefix = "Summary of the conversation so far:"
)

var slugPattern = regexp.MustCompile(`^[^/\\]+$`)

// Recall searches the user's memories and past conversations with BM25, so
// the model finds what it needs instead of carrying it in every request.
// Conversations are read once and kept until their file changes.
type Recall struct {
	sessionDir string
	mu         sync.Mutex
	files      map[string]recalledFile
	current    string
}

type recalledFile struct {
	modified time.Time
	info     sessions.Info
	turns    []recalledTurn
}

// recalledTurn is a prompt of a past conversation and what followed it.
type recalledTurn struct {
	prompt string
	text   string
}

// recallChunk is one thing that can be found: a memory or a turn.
type recallChunk struct {
	id, title string
	when      time.Time
	text      string
	terms     map[string]int
	length    int
}

func NewRecall(sessionDir string) *Recall {
	return &Recall{sessionDir: sessionDir, files: map[string]recalledFile{}}
}

// SetCurrent names the conversation in progress, left out of searches: the
// model has it already.
func (r *Recall) SetCurrent(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = id
}

func recallTool() Tool {
	return Tool{
		Name: recallName,
		Description: "Search your saved memories and the user's past conversations, then read what you need. " +
			"search: query words, scope here (this project, default) or all, kind memories or conversations (default both). " +
			"read: an id from the results; c:<conversation> lists its turns.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["search","read"]},"query":{"type":"string"},"id":{"type":"string"},
"scope":{"type":"string","enum":["here","all"]},"kind":{"type":"string","enum":["memories","conversations"]},"limit":{"type":"integer"}},"required":["action"]}`),
		Run: runRecall,
		Summary: func(args json.RawMessage) string {
			return strings.TrimSpace(argField(args, "action") + " " + argField(args, "query") + argField(args, "id"))
		},
	}
}

func runRecall(_ context.Context, env Env, raw json.RawMessage) (string, error) {
	if env.Recall == nil {
		return "", errors.New("nothing to recall here")
	}
	var args struct {
		Action, Query, ID, Scope, Kind string
		Limit                          int
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	switch args.Action {
	case "search":
		limit := args.Limit
		if limit <= 0 {
			limit = recallResults
		}
		return env.Recall.search(env, args.Query, args.Scope, args.Kind, min(limit, mostRecallResults))
	case "read":
		return env.Recall.read(env, args.ID)
	}
	return "", fmt.Errorf("action %q: use search or read", args.Action)
}

// search ranks memories and turns by BM25 and shows the best with a piece
// of text around where the query matched.
func (r *Recall) search(env Env, query, scope, kind string, limit int) (string, error) {
	words := tokens(query)
	if len(words) == 0 {
		return "", errors.New("search needs query words")
	}
	var chunks []recallChunk
	if kind != kindConversations {
		chunks = append(chunks, memoryChunks(env, scope == scopeAll)...)
	}
	if kind != kindMemories {
		chunks = append(chunks, r.conversationChunks(env, scope == scopeAll)...)
	}
	ranked := rank(chunks, words)
	if len(ranked) == 0 {
		return "nothing found for " + query + "; try other words, or scope all", nil
	}
	var out strings.Builder
	for _, chunk := range ranked[:min(limit, len(ranked))] {
		fmt.Fprintf(&out, "%s · %s · %s\n  %s\n", chunk.id, chunk.title, chunk.when.Format("2006-01-02"), snippet(chunk.text, words))
	}
	out.WriteString("read one whole with recall action=read id=<id>")
	return out.String(), nil
}

// rank scores chunks with BM25 and keeps those that match, best first.
func rank(chunks []recallChunk, words []string) []recallChunk {
	if len(chunks) == 0 {
		return nil
	}
	total := 0
	holding := map[string]int{}
	for _, chunk := range chunks {
		total += chunk.length
		for _, word := range words {
			if chunk.terms[word] > 0 {
				holding[word]++
			}
		}
	}
	average := float64(total) / float64(len(chunks))
	type scored struct {
		chunk recallChunk
		score float64
	}
	var found []scored
	for _, chunk := range chunks {
		score := 0.0
		for _, word := range slices.Compact(slices.Sorted(slices.Values(words))) {
			count := float64(chunk.terms[word])
			if count == 0 {
				continue
			}
			idf := math.Log(1 + (float64(len(chunks))-float64(holding[word])+0.5)/(float64(holding[word])+0.5))
			score += idf * count * (bm25K1 + 1) / (count + bm25K1*(1-bm25B+bm25B*float64(chunk.length)/average))
		}
		if score > 0 {
			found = append(found, scored{chunk, score})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return b.chunk.when.Compare(a.chunk.when)
	})
	ranked := make([]recallChunk, len(found))
	for i, each := range found {
		ranked[i] = each.chunk
	}
	return ranked
}

// tokens are the lower-case words of a text, letters, digits and _, two
// characters or more.
func tokens(text string) []string {
	var words []string
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_'
	}) {
		if len([]rune(word)) >= 2 {
			words = append(words, word)
		}
	}
	return words
}

func newChunk(id, title string, when time.Time, text string) recallChunk {
	words := tokens(title + " " + text)
	terms := map[string]int{}
	for _, word := range words {
		terms[word]++
	}
	return recallChunk{id: id, title: title, when: when, text: text, terms: terms, length: len(words)}
}

// snippet is a piece of text around the first query word in it, on one
// line.
func snippet(text string, words []string) string {
	flat := strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(flat)
	at := -1
	for _, word := range words {
		if i := strings.Index(lower, word); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	runes := []rune(flat)
	start := max(len([]rune(lower[:max(at, 0)]))-snippetLength/3, 0)
	end := min(start+snippetLength, len(runes))
	piece := string(runes[start:end])
	if start > 0 {
		piece = "…" + piece
	}
	if end < len(runes) {
		piece += "…"
	}
	return piece
}

// memoryChunks are the memories of this project and the global ones, or of
// every project.
func memoryChunks(env Env, everywhere bool) []recallChunk {
	if env.MemoryDir == "" {
		return nil
	}
	dirs := map[string]string{"project": filepath.Join(env.MemoryDir, "projects", projectSlug(env.WorkDir)), memoryScopeGlobal: filepath.Join(env.MemoryDir, memoryScopeGlobal)}
	if everywhere {
		projects, _ := os.ReadDir(filepath.Join(env.MemoryDir, "projects"))
		for _, project := range projects {
			if project.IsDir() && project.Name() != projectSlug(env.WorkDir) {
				dirs[project.Name()] = filepath.Join(env.MemoryDir, "projects", project.Name())
			}
		}
	}
	var chunks []recallChunk
	for scope, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			name, isMemory := strings.CutSuffix(entry.Name(), ".md")
			if !isMemory {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			info, statErr := entry.Info()
			if err != nil || statErr != nil {
				continue
			}
			chunks = append(chunks, newChunk(memoryPrefix+scope+"/"+name, "memory "+name, info.ModTime(), string(data)))
		}
	}
	return chunks
}

// conversationChunks are the turns of the saved conversations of this
// folder, or of every folder, but the one in progress.
func (r *Recall) conversationChunks(env Env, everywhere bool) []recallChunk {
	here := workDirKey(env.WorkDir)
	var chunks []recallChunk
	for _, file := range r.conversations() {
		if file.info.ID == r.currentID() || (!everywhere && file.info.WorkDir != here) {
			continue
		}
		for i, turn := range file.turns {
			id := fmt.Sprintf("%s%s%s%d", conversationPrefix, file.info.ID, turnSeparator, i+1)
			chunks = append(chunks, newChunk(id, `"`+file.info.Title+`"`, file.info.Updated, turn.text))
		}
	}
	return chunks
}

func (r *Recall) currentID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

// conversations reads the saved conversations, again only those whose file
// changed since.
func (r *Recall) conversations() []recalledFile {
	entries, _ := os.ReadDir(r.sessionDir)
	r.mu.Lock()
	defer r.mu.Unlock()
	var files []recalledFile
	for _, entry := range entries {
		id, isConversation := strings.CutSuffix(entry.Name(), ".json")
		info, err := entry.Info()
		if !isConversation || err != nil {
			continue
		}
		cached, known := r.files[id]
		if !known || !cached.modified.Equal(info.ModTime()) {
			record, err := sessions.Load(r.sessionDir, id)
			if err != nil {
				continue
			}
			cached = recalledFile{modified: info.ModTime(), info: record.Info, turns: turnsOf(record.History, recalledToolOutput)}
			r.files[id] = cached
		}
		files = append(files, cached)
	}
	return files
}

// turnsOf splits a history at the user's prompts: each turn is the prompt,
// the answers, and the tool calls with the start of their results.
func turnsOf(history []llm.Message, toolOutput int) []recalledTurn {
	var turns []recalledTurn
	var text strings.Builder
	prompt := ""
	flush := func() {
		if text.Len() > 0 {
			turns = append(turns, recalledTurn{prompt: prompt, text: text.String()})
			text.Reset()
		}
	}
	for _, message := range history {
		switch {
		case message.Role == llm.RoleUser && !strings.HasPrefix(message.Text, moNotePrefix):
			flush()
			prompt = message.Text
			if strings.HasPrefix(prompt, summaryHeaderPrefix) {
				prompt = "(summary of what came before)"
			}
			fmt.Fprintf(&text, "User: %s\n", message.Text)
		case message.Role == llm.RoleAssistant:
			if strings.TrimSpace(message.Text) != "" {
				fmt.Fprintf(&text, "Assistant: %s\n", message.Text)
			}
			for _, call := range message.ToolCalls {
				fmt.Fprintf(&text, "[%s %s]\n", call.Name, cut(string(call.Args), recalledToolInput))
			}
		case message.Role == llm.RoleTool:
			fmt.Fprintf(&text, "  → %s\n", cut(message.Text, toolOutput))
		}
	}
	flush()
	return turns
}

func cut(text string, length int) string {
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > length {
		return string(runes[:length]) + "…"
	}
	return text
}

// read gives a memory whole, a conversation's turns, or one turn whole.
func (r *Recall) read(env Env, id string) (string, error) {
	switch {
	case strings.HasPrefix(id, memoryPrefix):
		return readMemory(env, strings.TrimPrefix(id, memoryPrefix))
	case strings.HasPrefix(id, conversationPrefix):
		conversation, turn, hasTurn := strings.Cut(strings.TrimPrefix(id, conversationPrefix), turnSeparator)
		record, err := sessions.Load(r.sessionDir, conversation)
		if err != nil {
			return "", fmt.Errorf("no conversation %q", conversation)
		}
		turns := turnsOf(record.History, readToolOutput)
		if !hasTurn {
			return conversationTurns(record.Info, turns), nil
		}
		var number int
		if _, err := fmt.Sscan(turn, &number); err != nil || number < 1 || number > len(turns) {
			return "", fmt.Errorf("turn %q: the conversation has turns 1 to %d", turn, len(turns))
		}
		return fmt.Sprintf("%q · %s · turn %d of %d\n\n%s", record.Title, record.Updated.Format("2006-01-02"), number, len(turns), turns[number-1].text), nil
	}
	return "", fmt.Errorf("id %q: use one from a search, m:… or c:…", id)
}

func conversationTurns(info sessions.Info, turns []recalledTurn) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%q · %s · %s · %d turns\n", info.Title, info.WorkDir, info.Updated.Format("2006-01-02"), len(turns))
	for i, turn := range turns {
		fmt.Fprintf(&out, "%s%s%s%d: %s\n", conversationPrefix, info.ID, turnSeparator, i+1, cut(strings.Join(strings.Fields(turn.prompt), " "), 100))
	}
	return out.String()
}

func readMemory(env Env, name string) (string, error) {
	scope, note, found := strings.Cut(name, "/")
	if !found || !memoryNamePattern.MatchString(note) || !slugPattern.MatchString(scope) || scope == ".." || scope == "." {
		return "", fmt.Errorf("memory id %q: use one from a search", name)
	}
	dir := filepath.Join(env.MemoryDir, "projects", scope)
	switch scope {
	case "project":
		dir = filepath.Join(env.MemoryDir, "projects", projectSlug(env.WorkDir))
	case memoryScopeGlobal:
		dir = filepath.Join(env.MemoryDir, memoryScopeGlobal)
	}
	data, err := os.ReadFile(filepath.Join(dir, note+".md"))
	if err != nil {
		return "", fmt.Errorf("no memory %s", name)
	}
	return string(data), nil
}

// workDirKey names a folder as conversations are saved under it.
func workDirKey(workDir string) string {
	if absolute, err := filepath.Abs(workDir); err == nil {
		return absolute
	}
	return filepath.Clean(workDir)
}
