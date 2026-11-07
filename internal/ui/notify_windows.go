//go:build windows

package ui

import (
	"context"
	"os"
	"os/exec"
)

// toastScript shows a Windows toast. Title and body come from environment
// variables, so the text is never interpolated into the script.
const toastScript = `
$ErrorActionPreference = 'Stop'
[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$texts = $xml.GetElementsByTagName('text')
$texts.Item(0).AppendChild($xml.CreateTextNode($env:LB_TOAST_TITLE)) | Out-Null
$texts.Item(1).AppendChild($xml.CreateTextNode($env:LB_TOAST_BODY)) | Out-Null
$appId = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appId).Show([Windows.UI.Notifications.ToastNotification]::new($xml))
`

func showSystemNotification(ctx context.Context, title, body string) error {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", toastScript)
	cmd.Env = append(os.Environ(), "LB_TOAST_TITLE="+title, "LB_TOAST_BODY="+body)
	return cmd.Run()
}
