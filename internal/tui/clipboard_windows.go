package tui

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// windowsClipboardTimeout is how long Windows PowerShell may take to
// start and reach the clipboard.
const windowsClipboardTimeout = 10 * time.Second

// clipboardPrelude loads the clipboard's classes, reads mo's input as
// bytes and writes UTF-8.
const clipboardPrelude = `Add-Type -AssemblyName System.Windows.Forms, System.Drawing
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$in = New-Object IO.MemoryStream
[Console]::OpenStandardInput().CopyTo($in)
$C = [System.Windows.Forms.Clipboard]
`

const (
	copyTextScript = `$text = [Text.Encoding]::UTF8.GetString($in.ToArray()); if ($text) { $C::SetText($text) }`
	copyFileScript = `$files = New-Object System.Collections.Specialized.StringCollection
[Text.Encoding]::UTF8.GetString($in.ToArray()) -split "` + "`n" + `" | Where-Object { $_ } | ForEach-Object { [void]$files.Add($_) }
$C::SetFileDropList($files)`
	copyImageScript = `$data = New-Object System.Windows.Forms.DataObject
$data.SetImage([System.Drawing.Image]::FromStream($in))
$data.SetData('PNG', (New-Object IO.MemoryStream(,$in.ToArray())))
$C::SetDataObject($data, $true)`
	clipboardTypesScript = `if ($C::ContainsFileDropList()) { 'text/uri-list' }
if ($C::ContainsImage() -or $C::ContainsData('PNG')) { 'image/png' }`
	readFilesScript = `$C::GetFileDropList() | ForEach-Object { $_ }`
	readImageScript = `$png = $C::GetData('PNG')
if ($png -is [IO.MemoryStream]) { [IO.File]::WriteAllBytes($env:MO_CLIP_OUT, $png.ToArray()) }
else { $C::GetImage().Save($env:MO_CLIP_OUT, [System.Drawing.Imaging.ImageFormat]::Png) }
$env:MO_CLIP_OUT`
)

// windowsPowerShell is Windows PowerShell 5.1, which every Windows has and
// whose Windows Forms reach the clipboard.
func windowsPowerShell() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// clipboardScript runs a script on the clipboard with input on its stdin,
// in a single-threaded apartment, as the clipboard needs, and without a
// console, so it does not touch mo's.
func clipboardScript(ctx context.Context, script string, input []byte, env ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, windowsPowerShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", clipboardPrelude+script)
	command.Stdin = bytes.NewReader(input)
	command.Env = append(os.Environ(), env...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	var problem bytes.Buffer
	command.Stderr = &problem
	output, err := command.Output()
	if err != nil && problem.Len() > 0 {
		return output, errors.New(strings.TrimSpace(problem.String()))
	}
	return output, err
}

func osClipboard() (clipboard, bool) {
	if _, err := os.Stat(windowsPowerShell()); err != nil {
		return clipboard{}, false
	}
	return clipboard{
		timeout: windowsClipboardTimeout,
		toFile:  true,
		types: func(ctx context.Context) ([]string, error) {
			output, err := clipboardScript(ctx, clipboardTypesScript, nil)
			return strings.Fields(string(output)), err
		},
		read: func(ctx context.Context, mime string) ([]byte, error) {
			if mime == "image/png" {
				saved, err := os.CreateTemp("", "mo-paste-*.png")
				if err != nil {
					return nil, err
				}
				saved.Close()
				output, err := clipboardScript(ctx, readImageScript, nil, "MO_CLIP_OUT="+saved.Name())
				if err != nil {
					os.Remove(saved.Name())
				}
				return output, err
			}
			output, err := clipboardScript(ctx, readFilesScript, nil)
			var uris []string
			for _, path := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				if path = strings.TrimSpace(path); path != "" {
					uris = append(uris, fileURI(path))
				}
			}
			return []byte(strings.Join(uris, "\r\n")), err
		},
	}, true
}

func osCopy(item clipData) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowsClipboardTimeout)
	defer cancel()
	switch item.mime {
	case "":
		_, err := clipboardScript(ctx, copyTextScript, item.data)
		return true, err
	case "text/uri-list":
		var paths []string
		for line := range strings.Lines(string(item.data)) {
			if parsed, err := url.Parse(strings.TrimSpace(line)); err == nil && parsed.Scheme == "file" {
				paths = append(paths, localPath(parsed.Path))
			}
		}
		_, err := clipboardScript(ctx, copyFileScript, []byte(strings.Join(paths, "\n")))
		return true, err
	case "image/png":
		_, err := clipboardScript(ctx, copyImageScript, item.data)
		return true, err
	}
	return true, errors.New("Windows' clipboard takes text, files and pictures here, not " + item.mime)
}

func osCopies() bool {
	_, err := os.Stat(windowsPowerShell())
	return err == nil
}

// uriPath is a local path as a file:// URI writes it: C:\a\b as /C:/a/b.
func uriPath(path string) string {
	path = filepath.ToSlash(path)
	if filepath.VolumeName(filepath.FromSlash(path)) != "" && !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// localPath is the local path of a file:// URI's path: /C:/a/b as C:\a\b.
func localPath(path string) string {
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}
