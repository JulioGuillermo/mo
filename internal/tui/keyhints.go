package tui

// keyHint is a key, or several that do the same, and what it does.
type keyHint struct{ keys, does string }

var treeMoveHints = []keyHint{
	{"↑ ↓, j k", "move"},
	{"pgup pgdown", "a page"},
	{"home end, g G", "the first row, the last"},
	{"← h", "close the folder, or go to the one over the file"},
	{"c", "fold the folder here and every folder inside it"},
	{"C", "fold every folder of the tree"},
	{"/", "find files by name, * and ? as patterns; esc clears it"},
}

var leftTreeHints = append(append([]keyHint{}, treeMoveHints...),
	keyHint{"→ l", "open the folder"},
	keyHint{"enter, space, → l on a file, click", "show the file in the viewer while the agent goes on; esc there comes back"},
	keyHint{"esc, tab, ctrl+t", "back to the conversation, the tree kept"},
	keyHint{"wheel", "move along the tree"},
	keyHint{"/tree off, /tree width N", "hide it, set its columns"},
)

var viewerTreeHints = append(append([]keyHint{}, treeMoveHints...),
	keyHint{"→ l", "open the folder, or show the file and stay in the tree"},
	keyHint{"enter, space, click", "open or close the folder, or show the file and go to it"},
	keyHint{"tab", "go to the file"},
	keyHint{"t", "hide the tree"},
	keyHint{"esc, q", "close the viewer"},
)

var placeholderHints = []keyHint{
	{"tab, t", "go to the tree, or open it"},
	{"esc, q", "close the viewer"},
}

var scrollHints = []keyHint{
	{"pgup pgdown, space, shift+↑↓", "a page"},
	{"home end, g G", "the start, the end"},
	{"← → h l, shift+wheel", "across long lines"},
	{"/", "search; n N next and previous; esc clears it"},
}

var jobHints = append([]keyHint{
	{"↑ ↓, j k, wheel", "scroll a line"},
}, append(scrollHints,
	keyHint{"drag, y", "copy the lines"},
	keyHint{"b", "send the command to the background; the model goes on"},
	keyHint{"x", "kill the command"},
	keyHint{"esc, q", "close"},
)...)

var fileHints = append([]keyHint{
	{"↑ ↓, j k", "move the cursor"},
	{"v, shift+↑↓, drag", "select lines"},
	{"c", "comment the line, or the lines selected"},
	{"d", "delete the comment on the line"},
	{"i", "edit the lines; enter saves"},
	{"u", "undo the last edit"},
	{"a", "attach the file to your message"},
	{"y", "copy the lines selected"},
	{"B", "blame: who last changed the line, and in which commit"},
	{"H", "the file's history, commit by commit"},
	{"m", "markdown rendered, or its source"},
}, append(scrollHints,
	keyHint{"t", "the tree of the folder beside the file, or hide it"},
	keyHint{"tab", "go to the tree"},
	keyHint{"esc, q", "close"},
)...)

var diffHints = append([]keyHint{
	{"↑ ↓, j k", "move the cursor"},
	{"v, shift+↑↓", "select lines"},
	{"e, E", "on ⋯, show some of the lines hidden there, or all"},
	{"f", "the whole file, or the changes alone"},
	{"c", "comment the line, or the lines selected"},
	{"a", "attach the file to your message"},
	{"y", "copy the lines selected"},
	{"H", "the file's history, commit by commit"},
}, append(scrollHints,
	keyHint{"t", "the tree of the files changed, or hide it"},
	keyHint{"tab", "go to the tree"},
	keyHint{"esc, q", "close"},
)...)

var historyHints = append([]keyHint{
	{"ctrl+n, ctrl+p", "the next commit, the previous"},
	{"tab", "go to the list of commits"},
	{"t", "hide the list, or show it"},
	{"e, E", "on ⋯, show some of the lines hidden there, or all"},
	{"f", "the whole file, or the changes alone"},
	{"c", "comment the line, or the lines selected"},
	{"a", "attach the file to your message"},
	{"y", "copy the lines selected"},
}, append(scrollHints,
	keyHint{"esc, q", "back to the file"},
)...)

var commitListHints = []keyHint{
	{"↑ ↓, j k", "move"},
	{"pgup pgdown, home end, g G", "a page, the first, the last"},
	{"enter, → l, space", "show the commit"},
	{"/", "find commits by what they say"},
	{"ctrl+n, ctrl+p", "the next commit, the previous"},
	{"tab", "go to the changes"},
	{"t", "hide the list"},
	{"esc, q", "back to the file"},
}

var markdownHints = append([]keyHint{
	{"↑ ↓, j k, wheel", "scroll"},
	{"m, esc", "the source"},
	{"i", "pictures drawn, or named"},
	{"y", "copy the source"},
}, scrollHints...)

var drawingHints = []keyHint{
	{"drag, ↑↓ ←→, h j k l, wheel", "pan"},
	{"s", "its code, or the drawing"},
	{"/", "search; n N next and previous"},
	{"y", "copy"},
	{"esc, q", "close"},
}

var typingHints = []keyHint{
	{"enter", "keep it"},
	{"shift+enter", "a new line"},
	{"esc", "cancel"},
}

var agentViewHints = []keyHint{
	{"enter", "send the subagent a message"},
	{"esc", "stop it"},
	{"ctrl+x", "close this view; the subagent goes on"},
	{"pgup pgdown, shift+↑↓, ctrl+↑↓, wheel", "scroll"},
	{"click", "open or fold reasoning and tool calls"},
}

var keysHint = keyHint{"f1, or ? when not writing", "these keys, to search"}

// shownKeys are the keys of what is on the screen: its name, and each key
// with what it does there.
func (m model) shownKeys() (string, []keyHint) {
	title, hints := m.keysHere()
	if title == "conversation" {
		return title, hints
	}
	return title, append([]keyHint{keysHint}, hints...)
}

func (m model) keysHere() (string, []keyHint) {
	if m.agentView != nil {
		return "subagent", agentViewHints
	}
	if v := m.viewer; v != nil {
		return m.viewerKeys(v)
	}
	if m.leftTreeFocused() {
		return "files at the left", leftTreeHints
	}
	hints := make([]keyHint, 0, len(keyHelp))
	for _, each := range keyHelp {
		hints = append(hints, keyHint{each[0], each[1]})
	}
	return "conversation", hints
}

func (m model) viewerKeys(v *outputViewer) (string, []keyHint) {
	switch {
	case v.commenting:
		return "comment", append(typingHints, keyHint{"enter with nothing typed", "delete the comment"})
	case v.editing:
		return "edit", typingHints
	case m.treeShown() && v.treeFocus:
		return "file tree", viewerTreeHints
	case m.commitListShown() && v.history.focus:
		return "commits", commitListHints
	case v.placeholder:
		return "files", placeholderHints
	case v.markdown != nil:
		return "markdown", markdownHints
	case v.drawing != "":
		return "diagram", drawingHints
	case v.history != nil:
		return "history", historyHints
	case v.changes != nil:
		return "changes", diffHints
	case v.file != "":
		return "file", fileHints
	}
	return "command output", jobHints
}
