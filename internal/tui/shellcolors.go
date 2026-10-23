package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/alecthomas/chroma/v2"
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
)

var (
	shellNumber   = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	shellKeywords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "fi": true, "for": true, "while": true,
		"until": true, "do": true, "done": true, "case": true, "esac": true, "in": true, "function": true, "time": true, "!": true}
	shellOperators = []string{"&>>", "2>>", "&&", "||", ">>", "<<", "2>", "&>", ">&", "|", ";", "&", ">", "<", "(", ")"}
)

// shellParts is what each rune of a command is: the program of each
// command, its flags short and long, numbers, strings, variables, the
// operators and redirections, the paths, URLs and files that exist, and
// comments; the rest plain text.
func shellParts(command string, isFile func(word string) bool) []shellPart {
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
		case character == '#' && (i == 0 || unicode.IsSpace(runes[i-1])):
			end := i
			for end < len(runes) && runes[end] != '\n' {
				end++
			}
			mark(i, end, partComment)
			i = end
			continue
		}
		if operator := operatorAt(runes, i); operator != "" {
			end := i + len([]rune(operator))
			mark(i, end, partOperator)
			redirected = strings.ContainsAny(operator, "<>")
			expectProgram = !redirected
			i = end
			continue
		}
		end := wordEnd(runes, i)
		word := string(runes[i:end])
		switch {
		case redirected:
			markWord(runes, i, end, mark, partPath)
			redirected = false
		case expectProgram && isAssignment(word):
			name := strings.Index(word, "=")
			mark(i, i+name, partVariable)
			markWord(runes, i+name+1, end, mark, valuePart(word[name+1:], isFile))
		case expectProgram && shellKeywords[word]:
			mark(i, end, partKeyword)
		case expectProgram:
			markWord(runes, i, end, mark, partProgram)
			expectProgram = false
		case strings.HasPrefix(word, "--") && len(word) > 2:
			flagEnd := end
			if equals := strings.Index(word, "="); equals > 0 {
				flagEnd = i + len([]rune(word[:equals]))
				markWord(runes, flagEnd+1, end, mark, valuePart(word[equals+1:], isFile))
			}
			mark(i, flagEnd, partLongFlag)
		case strings.HasPrefix(word, "-") && len(word) > 1 && !shellNumber.MatchString(word):
			mark(i, end, partShortFlag)
		default:
			markWord(runes, i, end, mark, valuePart(word, isFile))
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
func markWord(runes []rune, from, to int, mark func(from, to int, part shellPart), part shellPart) {
	mark(from, to, part)
	for i := from; i < to; i++ {
		switch runes[i] {
		case '\'', '"':
			quote := runes[i]
			end := i + 1
			for end < to && runes[end] != quote {
				if runes[end] == '\\' && quote == '"' {
					end++
				}
				end++
			}
			end = min(end+1, to)
			mark(i, end, partString)
			if quote == '"' {
				markVariables(runes, i, end, mark)
			}
			i = end - 1
		case '$':
			i = markVariable(runes, i, to, mark) - 1
		}
	}
}

func markVariables(runes []rune, from, to int, mark func(from, to int, part shellPart)) {
	for i := from; i < to; i++ {
		if runes[i] == '$' {
			i = markVariable(runes, i, to, mark) - 1
		}
	}
}

// markVariable marks $NAME, ${...}, $1 or $? from i, and tells where it
// ends.
func markVariable(runes []rune, i, to int, mark func(from, to int, part shellPart)) int {
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
		for end < to && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_') {
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
func wordEnd(runes []rune, i int) int {
	var quote rune
	for ; i < len(runes); i++ {
		character := runes[i]
		switch {
		case quote != 0:
			if character == '\\' && quote == '"' {
				i++
			} else if character == quote {
				quote = 0
			}
		case character == '\'' || character == '"':
			quote = character
		case character == '\\':
			i++
		case unicode.IsSpace(character), operatorAt(runes, i) != "":
			return i
		}
	}
	return min(i, len(runes))
}

// operatorAt is the operator or redirection that starts at i, if any.
func operatorAt(runes []rune, i int) string {
	rest := string(runes[i:min(i+3, len(runes))])
	for _, operator := range shellOperators {
		if strings.HasPrefix(rest, operator) {
			if strings.HasPrefix(operator, "2") && i > 0 && !unicode.IsSpace(runes[i-1]) {
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

// shellLanguage is the language of the commands of the shell, typed or run
// by the model, coloured by shellParts instead of chroma.
const shellLanguage = "bash"

var partKinds = map[shellPart]chroma.TokenType{
	partText: chroma.Text, partProgram: kindProgram, partShortFlag: kindShortFlag, partLongFlag: kindLongFlag,
	partNumber: chroma.LiteralNumber, partString: chroma.LiteralString, partVariable: chroma.NameVariable,
	partOperator: chroma.Operator, partPath: kindPath, partComment: chroma.Comment, partKeyword: chroma.Keyword,
}

// shellTokens are the pieces of a command, each a run of the same part, as
// chroma gives them for other languages.
func shellTokens(command string) []chroma.Token {
	runes := []rune(command)
	parts := shellParts(command, fileExists)
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
