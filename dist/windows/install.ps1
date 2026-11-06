[CmdletBinding()]
param(
    [string]$PackageDirectory,
    [string]$InstallDirectory = (Join-Path $env:LOCALAPPDATA "LanBridge"),
    [string]$ConfigDirectory = (Join-Path $env:APPDATA "LanBridge"),
    [string]$ShortcutRoot
)

$ErrorActionPreference = "Stop"

try {
    if ([string]::IsNullOrWhiteSpace($PackageDirectory)) {
        $PackageDirectory = $PSScriptRoot
    }
    $packageExe = Join-Path $PackageDirectory "bridge.exe"
    if (-not (Test-Path -LiteralPath $packageExe -PathType Leaf)) {
        throw "bridge.exe was not found in package directory: $PackageDirectory"
    }
    $packageIcon = Join-Path $PackageDirectory "lanbridge.ico"
    if (-not (Test-Path -LiteralPath $packageIcon -PathType Leaf)) {
        throw "lanbridge.ico was not found in package directory: $PackageDirectory"
    }

    New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
    New-Item -ItemType Directory -Force -Path $ConfigDirectory | Out-Null
    $installedExe = Join-Path $InstallDirectory "bridge.exe"
    Copy-Item -LiteralPath $packageExe -Destination $installedExe -Force
    $installedIcon = Join-Path $InstallDirectory "lanbridge.ico"
    Copy-Item -LiteralPath $packageIcon -Destination $installedIcon -Force

    $configPath = Join-Path $ConfigDirectory "config.json"
    if (-not (Test-Path -LiteralPath $configPath)) {
        $sampleConfig = @'
{
  "port": 54823,
  "max_sync_mb": 5,
  "projects": [],
  "apps": []
}
'@
        $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
        [System.IO.File]::WriteAllText($configPath, $sampleConfig, $utf8NoBom)
    }

    if ($ShortcutRoot) {
        $desktopDirectory = Join-Path $ShortcutRoot "Desktop"
        $startMenuDirectory = Join-Path $ShortcutRoot "Start Menu\Programs\LAN Bridge"
    }
    else {
        $desktopDirectory = [Environment]::GetFolderPath([Environment+SpecialFolder]::DesktopDirectory)
        if ([string]::IsNullOrWhiteSpace($desktopDirectory)) {
            if ([string]::IsNullOrWhiteSpace($env:USERPROFILE)) {
                throw "Cannot locate the Windows Desktop: DesktopDirectory and USERPROFILE are empty."
            }
            $desktopDirectory = Join-Path $env:USERPROFILE "Desktop"
        }
        $startMenuDirectory = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\LAN Bridge"
    }
    New-Item -ItemType Directory -Force -Path $desktopDirectory, $startMenuDirectory | Out-Null

    $shell = New-Object -ComObject WScript.Shell
    foreach ($shortcutPath in @(
        (Join-Path $desktopDirectory "LAN Bridge.lnk"),
        (Join-Path $startMenuDirectory "LAN Bridge.lnk")
    )) {
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = $installedExe
        $shortcut.Arguments = "host"
        $shortcut.WorkingDirectory = $InstallDirectory
        $shortcut.IconLocation = "$installedIcon,0"
        $shortcut.Save()
    }

    $getProfile = Get-Command Get-NetConnectionProfile -ErrorAction SilentlyContinue
    if ($getProfile) {
        try {
            $wifiProfiles = @(Get-NetConnectionProfile -ErrorAction Stop | Where-Object {
                $_.InterfaceAlias -match "(?i)wi-?fi|wlan|wireless"
            })
            $publicProfiles = @($wifiProfiles | Where-Object {
                $_.NetworkCategory -eq "Public"
            })
            if ($publicProfiles.Count -gt 0) {
                $names = ($publicProfiles | ForEach-Object { $_.InterfaceAlias } | Select-Object -Unique) -join ", "
                Write-Warning "Network profile(s) are Public: $names. On a trusted network, change Wi-Fi to Private in Settings > Network & Internet > Wi-Fi > network properties."
            }
        }
        catch {
            Write-Warning "Could not inspect Windows network profiles: $($_.Exception.Message)"
        }
    }
    else {
        Write-Warning "Get-NetConnectionProfile is unavailable; verify the active network is Private before pairing."
    }

    Write-Host "Installed LAN Bridge at: $InstallDirectory"
    Write-Host "Configuration: $configPath"
    Write-Host "Start LAN Bridge from the Desktop or Start Menu shortcut."
    Write-Host "On first start, allow Windows Firewall access only for trusted Private networks."
}
catch {
    Write-Error "LAN Bridge installation failed: $($_.Exception.Message)`n$($_.ScriptStackTrace)"
    exit 1
}
