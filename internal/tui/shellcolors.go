package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/alecthomas/chroma/v2"

	"github.com/jg/mo/internal/tools"
)

// shellPart is what a rune of a command is, to colour it.
type shellPart int

const (
	partText shellPart = iota
	partProgram
	partShortFlag
	partLongFlag
	partNumber
	partString
	partVariable
	partOperator
	partPath
	partComment
	partKeyword
	partType
)

var shellNumber = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// shellDialect is what tells one shell's commands apart from another's:
// the character that escapes, the keywords, the operators and
// redirections, the words that are operators, and whether <# #> comments,
// [Type]s and $scope:names are written.
type shellDialect struct {
	language      string
	escape        rune
	keywords      map[string]bool
	operators     []string
	operatorWords map[string]bool
	blockComments bool
	types         bool
	scopedNames   bool
}

var (
	bashDialect = &shellDialect{
		language: "bash", escape: '\\',
		keywords: map[string]bool{"if": true, "then": true, "else": true, "elif": true, "fi": true, "for": true, "while": true,
			"until": true, "do": true, "done": true, "case": true, "esac": true, "in": true, "function": true, "time": true, "!": true},
		operators: []string{"&>>", "2>>", "&&", "||", ">>", "<<", "2>", "&>", ">&", "|", ";", "&", ">", "<", "(", ")"},
	}
	powershellDialect = &shellDialect{
		language: "powershell", escape: '`',
		keywords: map[string]bool{"if": true, "else": true, "elseif": true, "foreach": true, "for": true, "while": true, "do": true,
			"until": true, "switch": true, "function": true, "filter": true, "param": true, "return": true, "try": true, "catch": true,
			"finally": true, "throw": true, "break": true, "continue": true, "begin": true, "process": true, "end": true, "in": true,
			"exit": true, "trap": true, "class": true, "enum": true, "using": true},
		operators: []string{"2>&1", "*>>", "2>>", "*>", "&&", "||", ">>", "2>", "|", ";", "&", ">", "(", ")", "{", "}"},
		operatorWords: map[string]bool{"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "-eq": true, "-ne": true,
			"-gt": true, "-ge": true, "-lt": true, "-le": true, "-like": true, "-notlike": true, "-match": true, "-notmatch": true,
			"-contains": true, "-notcontains": true, "-in": true, "-notin": true, "-and": true, "-or": true, "-xor": true, "-not": true,
			"-is": true, "-isnot": true, "-as": true, "-replace": true, "-split": true, "-join": true, "-f": true, "-band": true, "-bor": true,
			"-ceq": true, "-ieq": true, "-cmatch": true, "-imatch": true, "-clike": true, "-ilike": true, "!": true},
		blockComments: true, types: true, scopedNames: true,
	}
)

// shellDialectFor is the dialect of a language mo colours itself, if it is
// a shell's.
func shellDialectFor(language string) (*shellDialect, bool) {
	switch strings.ToLower(language) {
	case "bash", "sh", "shell", "zsh":
		return bashDialect, true
	case "powershell", "pwsh", "ps1":
		return powershellDialect, true
	}
	return nil, false
}

// shellParts is what each rune of a bash command is.
func shellParts(command string, isFile func(word string) bool) []shellPart {
	return bashDialect.parts(command, isFile)
}

// parts is what each rune of a command is: the program of each command,
// its flags short and long, numbers, strings, variables, the operators and
// redirections, the paths, URLs and files that exist, keywords, [Type]s
// and comments; the rest plain text.
func (d *shellDialect) parts(command string, isFile func(word string) bool) []shellPart {
	runes := []rune(command)
	parts := make([]shellPart, len(runes))
	mark := func(from, to int, part shellPart) {
		for i := from; i < to; i++ {
			parts[i] = part
		}
	}
	expectProgram, redirected := true, false
	for i := 0; i < len(runes); {
		character := runes[i]
		switch {
		case unicode.IsSpace(character):
			if character == '\n' {
				expectProgram = true
			}
			i++
			continue
		case d.blockComments && strings.HasPrefix(string(runes[i:min(i+2, len(runes))]), "<#"):
			end := strings.Index(string(runes[i:]), "#>")
			stop := len(runes)
			if end >= 0 {
				stop = i + len([]rune(string(runes[i:])[:end])) + 2
			}
			mark(i, stop, partComment)
			i = stop
			continue
		case character == '#' && (i == 0 || unicode.IsSpace(runes[i-1])):
			end := i
			for end < len(runes) && runes[end] != '\n' {
				end++
			}
			mark(i, end, partComment)
			i = end
			continue
		}
		if operator := d.operatorAt(runes, i); operator != "" {
			end := i + len([]rune(operator))
			mark(i, end, partOperator)
			redirected = strings.ContainsAny(operator, "<>") && operator != "2>&1"
			expectProgram = !redirected && operator != ")" && operator != "}"
			i = end
			continue
		}
		end := d.wordEnd(runes, i)
		word := string(runes[i:end])
		switch {
		case redirected:
			d.markWord(runes, i, end, mark, partPath)
			redirected = false
		case d.operatorWords[strings.ToLower(word)]:
			mark(i, end, partOperator)
			expectProgram = strings.HasSuffix(word, "=")
		case d.types && strings.HasPrefix(word, "["):
			typeEnd := i + strings.Index(word, "]") + 1
			if typeEnd <= i {
				typeEnd = end
			}
			mark(i, typeEnd, partType)
			d.markWord(runes, typeEnd, end, mark, partText)
		case expectProgram && d == bashDialect && isAssignment(word):
			name := strings.Index(word, "=")
			mark(i, i+name, partVariable)
			d.markWord(runes, i+name+1, end, mark, valuePart(word[name+1:], isFile))
		case expectProgram && d.keywords[strings.ToLower(word)]:
			mark(i, end, partKeyword)
		case expectProgram && strings.HasPrefix(word, "$"):
			d.markWord(runes, i, end, mark, partText)
		case expectProgram && shellNumber.MatchString(word):
			mark(i, end, partNumber)
			expectProgram = false
		case expectProgram:
			d.markWord(runes, i, end, mark, partProgram)
			expectProgram = false
		case strings.HasPrefix(word, "--") && len(word) > 2:
			flagEnd := end
			if equals := strings.Index(word, "="); equals > 0 {
				flagEnd = i + len([]rune(word[:equals]))
				d.markWord(runes, flagEnd+1, end, mark, valuePart(word[equals+1:], isFile))
			}
			mark(i, flagEnd, partLongFlag)
		case strings.HasPrefix(word, "-") && len(word) > 1 && !shellNumber.MatchString(word):
			flagEnd := end
			if colon := strings.Index(word, ":"); d.scopedNames && colon > 0 {
				flagEnd = i + len([]rune(word[:colon]))
				d.markWord(runes, flagEnd+1, end, mark, valuePart(word[colon+1:], isFile))
			}
			mark(i, flagEnd, partShortFlag)
		default:
			d.markWord(runes, i, end, mark, valuePart(word, isFile))
		}
		i = end
	}
	return parts
}

// valuePart is what an argument is: a number, a path, a URL, a file that
// exists, or plain text.
func valuePart(word string, isFile func(string) bool) shellPart {
	bare := strings.Trim(word, `"'`)
	switch {
	case shellNumber.MatchString(bare):
		return partNumber
	case strings.Contains(bare, "://"), strings.ContainsAny(bare, `/\*`), strings.HasPrefix(bare, "~"):
		return partPath
	case bare != "" && !strings.ContainsAny(bare, "$`") && isFile(bare):
		return partPath
	}
	return partText
}

// markWord marks the runes of a word as part, but its quoted strings as
// strings and its variables as variables.
func (d *shellDialect) markWord(runes []rune, from, to int, mark func(from, to int, part shellPart), part shellPart) {
	mark(from, to, part)
	for i := from; i < to; i++ {
		switch runes[i] {
		case '\'', '"':
			quote := runes[i]
			end := i + 1
			for end < to && runes[end] != quote {
				if runes[end] == d.escape && quote == '"' {
					end++
				}
				end++
			}
			end = min(end+1, to)
			mark(i, end, partString)
			if quote == '"' {
				d.markVariables(runes, i, end, mark)
			}
			i = end - 1
		case '$':
			i = d.markVariable(runes, i, to, mark) - 1
		}
	}
}

func (d *shellDialect) markVariables(runes []rune, from, to int, mark func(from, to int, part shellPart)) {
	for i := from; i < to; i++ {
		if runes[i] == '$' {
			i = d.markVariable(runes, i, to, mark) - 1
		}
	}
}

// markVariable marks $NAME, ${...}, $1 or $? from i, and in PowerShell
// $env:NAME, and tells where it ends.
func (d *shellDialect) markVariable(runes []rune, i, to int, mark func(from, to int, part shellPart)) int {
	end := i + 1
	switch {
	case end < to && runes[end] == '{':
		for end < to && runes[end] != '}' {
			end++
		}
		end = min(end+1, to)
	case end < to && strings.ContainsRune("?!#@*$-0123456789", runes[end]):
		end++
	default:
		for end < to && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_' ||
			(d.scopedNames && runes[end] == ':' && end+1 < to && unicode.IsLetter(runes[end+1]))) {
			end++
		}
	}
	if end > i+1 {
		mark(i, end, partVariable)
	}
	return end
}

// wordEnd is where the word from i ends: at a space or an operator outside
// quotes.
func (d *shellDialect) wordEnd(runes []rune, i int) int {
	var quote rune
	for ; i < len(runes); i++ {
		character := runes[i]
		switch {
		case quote != 0:
			if character == d.escape && quote == '"' {
				i++
			} else if character == quote {
				quote = 0
			}
		case character == '\'' || character == '"':
			quote = character
		case character == d.escape:
			i++
		case unicode.IsSpace(character), d.operatorAt(runes, i) != "":
			return i
		}
	}
	return min(i, len(runes))
}

// operatorAt is the operator or redirection that starts at i, if any; a
// redirection of a stream, as 2>, only where a word starts.
func (d *shellDialect) operatorAt(runes []rune, i int) string {
	rest := string(runes[i:min(i+4, len(runes))])
	for _, operator := range d.operators {
		if strings.HasPrefix(rest, operator) {
			if (strings.HasPrefix(operator, "2") || strings.HasPrefix(operator, "*")) && i > 0 && !unicode.IsSpace(runes[i-1]) {
				continue
			}
			return operator
		}
	}
	return ""
}

func isAssignment(word string) bool {
	name, _, found := strings.Cut(word, "=")
	if !found || name == "" {
		return false
	}
	for i, character := range name {
		if !(unicode.IsLetter(character) || character == '_' || (i > 0 && unicode.IsDigit(character))) {
			return false
		}
	}
	return true
}

// shellLanguage is the language of bash commands, coloured by mo; so are
// PowerShell's, as shellDialectFor says.
const shellLanguage = "bash"

var partKinds = map[shellPart]chroma.TokenType{
	partText: chroma.Text, partProgram: kindProgram, partShortFlag: kindShortFlag, partLongFlag: kindLongFlag,
	partNumber: chroma.LiteralNumber, partString: chroma.LiteralString, partVariable: chroma.NameVariable,
	partOperator: chroma.Operator, partPath: kindPath, partComment: chroma.Comment, partKeyword: chroma.Keyword,
	partType: chroma.KeywordType,
}

// shellTokens are the pieces of a command of the shell mo runs commands
// in.
func shellTokens(command string) []chroma.Token {
	dialect, ok := shellDialectFor(tools.CurrentShell().Language)
	if !ok {
		dialect = bashDialect
	}
	return dialect.tokens(command)
}

// tokens are the pieces of a command, each a run of the same part, as
// chroma gives them for other languages.
func (d *shellDialect) tokens(command string) []chroma.Token {
	runes := []rune(command)
	parts := d.parts(command, fileExists)
	var tokens []chroma.Token
	for start := 0; start < len(runes); {
		end := start
		for end < len(runes) && parts[end] == parts[start] && (runes[end] == '\n') == (runes[start] == '\n') {
			end++
		}
		kind := partKinds[parts[start]]
		if runes[start] == '\n' {
			kind = chroma.Text
		}
		tokens = append(tokens, chroma.Token{Type: kind, Value: string(runes[start:end])})
		start = end
	}
	return tokens
}

// fileExists tells whether a word names a file or a folder from where mo
// runs, ~ the home folder.
func fileExists(word string) bool {
	if rest, ok := strings.CutPrefix(word, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			word = filepath.Join(home, rest)
		}
	}
	_, err := os.Stat(word)
	return err == nil
}
