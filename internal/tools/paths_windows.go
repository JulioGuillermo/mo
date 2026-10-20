package tools

import (
	"path/filepath"
	"strings"
)

// systemPath is a path as Windows writes it: /c/Users/me, as Git bash and
// MSYS write it, or /cygdrive/c/Users/me, is C:\Users\me; slashes either
// way are taken.
func systemPath(path string) string {
	unix := filepath.ToSlash(path)
	unix = strings.TrimPrefix(unix, "/cygdrive")
	if len(unix) >= 2 && unix[0] == '/' && isDriveLetter(unix[1]) && (len(unix) == 2 || unix[2] == '/') {
		return filepath.FromSlash(strings.ToUpper(unix[1:2]) + ":/" + strings.TrimPrefix(unix[2:], "/"))
	}
	return path
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
