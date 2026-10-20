//go:build !windows

package tools

func detectShell() Shell {
	return bashShell("bash")
}
