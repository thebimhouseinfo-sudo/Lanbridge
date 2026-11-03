//go:build windows

package ui

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Use an owned, topmost dialog so it is visible above the browser. Suppress
// the helper console separately; -WindowStyle Hidden also hides dialog owners.
func nativeWindowsPicker(ctx context.Context, kind string) (string, error) {
	dialog := `$d = New-Object System.Windows.Forms.FolderBrowserDialog`
	value := `$d.SelectedPath`
	if kind == "app" {
		dialog = `$d = New-Object System.Windows.Forms.OpenFileDialog; $d.Filter = 'Executable (*.exe)|*.exe|All files (*.*)|*.*'`
		value = `$d.FileName`
	}
	script := `$ErrorActionPreference = 'Stop'; [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); Add-Type -AssemblyName System.Windows.Forms; ` +
		`$owner = New-Object System.Windows.Forms.Form; $owner.Text = 'LAN Bridge'; $owner.ShowInTaskbar = $true; $owner.TopMost = $true; $owner.StartPosition = 'CenterScreen'; $owner.Size = New-Object System.Drawing.Size(1,1); $owner.Opacity = 0; ` + dialog +
		`; try { $owner.Show(); $owner.Activate(); if ($d.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write(` + value + `) } } finally { $d.Dispose(); $owner.Dispose() }`
	encoded := utf16.Encode([]rune(script))
	buf := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", base64.StdEncoding.EncodeToString(buf))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("hộp chọn hết thời gian hoặc đã bị hủy: %w", ctx.Err())
		}
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
