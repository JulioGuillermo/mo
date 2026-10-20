package permissions

import "strings"

// powershellAliases are PowerShell's built-in aliases, by lower case, with
// the cmdlet each stands for.
var powershellAliases = map[string]string{
	"ac": "Add-Content", "cat": "Get-Content", "gc": "Get-Content", "type": "Get-Content",
	"cd": "Set-Location", "chdir": "Set-Location", "sl": "Set-Location",
	"clc": "Clear-Content", "clear": "Clear-Host", "cls": "Clear-Host",
	"copy": "Copy-Item", "cp": "Copy-Item", "cpi": "Copy-Item",
	"del": "Remove-Item", "erase": "Remove-Item", "rd": "Remove-Item", "ri": "Remove-Item", "rm": "Remove-Item", "rmdir": "Remove-Item",
	"dir": "Get-ChildItem", "gci": "Get-ChildItem", "ls": "Get-ChildItem",
	"echo": "Write-Output", "write": "Write-Output",
	"gi": "Get-Item", "gl": "Get-Location", "pwd": "Get-Location",
	"gps": "Get-Process", "ps": "Get-Process", "gsv": "Get-Service",
	"gcm": "Get-Command", "gm": "Get-Member", "ghy": "Get-History", "h": "Get-History", "history": "Get-History",
	"iex": "Invoke-Expression", "icm": "Invoke-Command", "ii": "Invoke-Item",
	"irm": "Invoke-RestMethod", "iwr": "Invoke-WebRequest",
	"kill": "Stop-Process", "spps": "Stop-Process",
	"mi": "Move-Item", "move": "Move-Item", "mv": "Move-Item",
	"ni": "New-Item", "ren": "Rename-Item", "rni": "Rename-Item",
	"popd": "Pop-Location", "pushd": "Push-Location", "rvpa": "Resolve-Path",
	"saps": "Start-Process", "start": "Start-Process", "sajb": "Start-Job",
	"select": "Select-Object", "sort": "Sort-Object", "where": "Where-Object", "?": "Where-Object",
	"foreach": "ForEach-Object", "%": "ForEach-Object", "measure": "Measure-Object", "group": "Group-Object",
	"tee": "Tee-Object", "sls": "Select-String", "si": "Set-Item", "sp": "Set-ItemProperty", "gp": "Get-ItemProperty",
	"ft": "Format-Table", "fl": "Format-List", "compare": "Compare-Object", "diff": "Compare-Object",
	"sleep": "Start-Sleep", "ipmo": "Import-Module", "nal": "New-Alias", "sal": "Set-Alias",
	"set": "Set-Variable", "sv": "Set-Variable", "gv": "Get-Variable", "rv": "Remove-Variable",
}

// hiddenCmdlets run code given as text, a script block elsewhere or
// another program, which no rule can judge from the command.
var hiddenCmdlets = map[string]bool{
	"invoke-expression": true, "invoke-command": true, "start-process": true, "start-job": true,
	"start-threadjob": true, "add-type": true, "invoke-item": true, "register-scheduledjob": true,
	"register-scheduledtask": true, "new-pssession": true, "enter-pssession": true,
	"invoke-wmimethod": true, "invoke-cimmethod": true,
}

// cmdletOf is the cmdlet an alias stands for, or the name itself.
func cmdletOf(name string) string {
	if cmdlet, isAlias := powershellAliases[strings.ToLower(name)]; isAlias {
		return cmdlet
	}
	return name
}

// powershellRule is a rule's text as it is matched against PowerShell
// commands: its first word, an alias, as its cmdlet.
func powershellRule(text string) string {
	first, rest, _ := strings.Cut(text, " ")
	if strings.Contains(first, "*") || strings.HasPrefix(text, regexPrefix) {
		return text
	}
	return strings.TrimSpace(cmdletOf(first) + " " + rest)
}
