//go:build !windows

package notify

// systemDesktop is the desktop's notification where the system has one of
// its own to call; elsewhere a program is looked for.
func systemDesktop() func(title, body string) error {
	return nil
}
