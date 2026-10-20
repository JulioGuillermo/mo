//go:build !windows

package tools

// systemPath is a path as this system writes it.
func systemPath(path string) string {
	return path
}
