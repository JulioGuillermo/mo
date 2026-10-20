//go:build !windows

package render

import (
	"os"

	"golang.org/x/sys/unix"
)

func terminalCellSize() (width, height int, ok bool) {
	size, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 || size.Row == 0 || size.Xpixel < size.Col || size.Ypixel < size.Row {
		return 0, 0, false
	}
	return int(size.Xpixel) / int(size.Col), int(size.Ypixel) / int(size.Row), true
}
