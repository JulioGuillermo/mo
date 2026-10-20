package notify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// toastTimeout is how long Windows PowerShell may take to start and show
// the toast.
const toastTimeout = 15 * time.Second

// toastScript shows a toast with the title and body from the environment,
// as PowerShell's own app, which Windows lets show them unregistered.
const toastScript = `$m = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$null = $x.Item(0).AppendChild($t.CreateTextNode($env:MO_NOTIFY_TITLE))
$null = $x.Item(1).AppendChild($t.CreateTextNode($env:MO_NOTIFY_BODY))
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($t))`

// systemDesktop is a Windows toast, shown by Windows PowerShell 5.1, which
// every Windows has and which reaches the WinRT notifications.
func systemDesktop() func(title, body string) error {
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(powershell); err != nil {
		return nil
	}
	return func(title, body string) error {
		ctx, cancel := context.WithTimeout(context.Background(), toastTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", toastScript)
		command.Env = append(os.Environ(), "MO_NOTIFY_TITLE="+title, "MO_NOTIFY_BODY="+body)
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		return command.Run()
	}
}
