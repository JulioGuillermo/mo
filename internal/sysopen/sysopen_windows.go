package sysopen

import "golang.org/x/sys/windows"

// Open opens target, a path or an address, with the system's app for it,
// as the Explorer's double click does.
func Open(target string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
