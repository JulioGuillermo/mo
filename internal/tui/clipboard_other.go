//go:build !windows

package tui

// osClipboard is the clipboard the system has besides the programs mo
// looks for; there is none here.
func osClipboard() (clipboard, bool) {
	return clipboard{}, false
}

// osCopy copies with the system's own clipboard, telling whether it did
// the work; here the programs do.
func osCopy(clipData) (bool, error) {
	return false, nil
}

// osCopies tells whether the system copies text by itself.
func osCopies() bool {
	return false
}

// uriPath is a local path as a file:// URI writes it.
func uriPath(path string) string {
	return path
}

// localPath is the local path of a file:// URI's path.
func localPath(path string) string {
	return path
}
