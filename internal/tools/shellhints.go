package tools

type shellHintKind int

const (
	hintPart shellHintKind = iota
	hintUTF16
	hintBinary
)

var bashHints = map[shellHintKind]string{
	hintPart:   "bash (head, tail, grep)",
	hintUTF16:  "bash, e.g. iconv -f UTF-16 -t UTF-8 | head",
	hintBinary: "bash, e.g. file, xxd | head or strings | head",
}

var powershellHints = map[shellHintKind]string{
	hintPart:   "powershell (Get-Content -TotalCount or -Tail, Select-String)",
	hintUTF16:  "powershell, e.g. Get-Content -Encoding Unicode -TotalCount 100",
	hintBinary: "powershell, e.g. Format-Hex -Path file | Select-Object -First 20",
}

// shellHint is how to go on in the shell commands run in here.
func shellHint(kind shellHintKind) string {
	if CurrentShell().PowerShell() {
		return powershellHints[kind]
	}
	return bashHints[kind]
}
