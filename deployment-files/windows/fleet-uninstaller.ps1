# Proto Fleet - Windows Uninstaller (WSL/Docker + Fleet)
<#
.SYNOPSIS
    Uninstalls Proto Fleet on Windows without removing WSL or Docker.

.DESCRIPTION
    EXE-first uninstaller intended for ps2exe. Uses GUI prompts when interactive
    and console fallbacks when not. Removes only Proto Fleet artifacts.

.PARAMETER DeploymentPath
    Optional path to deployment root (WSL or Windows path).

.PARAMETER InstallDir
    Optional install dir in WSL (default: ~/proto-fleet).

.PARAMETER WslDistro
    WSL distribution name (default: Ubuntu).

.PARAMETER RetainData
    Keep data volumes (remove containers/images/deployment files).

.PARAMETER Clean
    Remove volumes as well (full uninstall).

.PARAMETER Force
    Skip confirmations other than the front-door prompt.

.PARAMETER Silent
    Non-interactive; requires -RetainData or -Clean.

.PARAMETER WhatIf
    Show actions without executing.
#>

[CmdletBinding()]
param(
    [string]$DeploymentPath = "",
    [string]$InstallDir = "~/proto-fleet",
    [string]$WslDistro = "",
    [switch]$RetainData,
    [switch]$Clean,
    [switch]$Force,
    [switch]$Silent,
    [switch]$WhatIf
)

$ErrorActionPreference = "Stop"

[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = [Console]::OutputEncoding

$script:UseGuiPrompts = $false
$script:IsExe = $false
$script:WslDistro = ""
$script:TranscriptStarted = $false
$script:LogPath = ""


# =====================================================================
# Output Helpers
# =====================================================================

function Write-Step {
    param([string]$Message)
    [Console]::Write("`r")
    Write-Host ""
    [Console]::Write("`r")
    Write-Host $Message -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    [Console]::Write("`r")
    Write-Host ("[OK] " + $Message) -ForegroundColor Green
}

function Write-WarningMsg {
    param([string]$Message)
    [Console]::Write("`r")
    Write-Host ("[WARN] " + $Message) -ForegroundColor Yellow
}

function Write-ErrorMsg {
    param([string]$Message)
    [Console]::Write("`r")
    Write-Host ("[ERROR] " + $Message) -ForegroundColor Red
}

function Write-Block {
    param([string[]]$Lines)
    [Console]::Write("`r")
    Write-Host ""
    foreach ($line in $Lines) {
        [Console]::Write("`r")
        Write-Host $line
    }
}

function Write-Log {
    param([string]$Message)
    if ([string]::IsNullOrWhiteSpace($script:LogPath)) { return }
    $ts = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
    Add-Content -LiteralPath $script:LogPath -Value ("[$ts] " + $Message) -ErrorAction SilentlyContinue
}

$script:LogPath = Join-Path $env:TEMP "protofleet-uninstall.log"
Write-Log "Startup: begin (pre-exe-detect)"

function Show-GuiMessage {
    param(
        [string]$Text,
        [string]$Title = "Proto Fleet Uninstaller",
        [string]$Icon = "Information"
    )
    try {
        Add-Type -AssemblyName System.Windows.Forms | Out-Null
        $iconEnum = [System.Windows.Forms.MessageBoxIcon]::Information
        if ($Icon -eq "Error") { $iconEnum = [System.Windows.Forms.MessageBoxIcon]::Error }
        [System.Windows.Forms.MessageBox]::Show($Text, $Title, [System.Windows.Forms.MessageBoxButtons]::OK, $iconEnum) | Out-Null
        return $true
    }
    catch {
        return $false
    }
}

function Invoke-Exit {
    param([int]$Code = 0)

    if ($script:IsExe -and -not $env:PROTOFLEET_NO_PAUSE) {
        if (-not (Show-GuiMessage -Text "Press OK to exit.")) {
            [Console]::Write("`r")
            Write-Host ""
            [Console]::Write("`r")
            [Console]::Write("Press Enter to exit")
            try {
                if ($host -and $host.UI -and $host.UI.RawUI) {
                    $host.UI.RawUI.FlushInputBuffer()
                }
                [Console]::ReadLine() | Out-Null
            }
            catch {
                Start-Sleep -Seconds 2
            }
        }
    }

    if ($script:TranscriptStarted) {
        try { Stop-Transcript | Out-Null } catch { }
    }

    exit $Code
}

trap {
    $msg = $_.Exception.Message
    Write-ErrorMsg "Unhandled error: $msg"
    Write-Host $_.Exception.ToString()
    Write-Log ("Unhandled error: " + $msg)
    Write-Log $_.Exception.ToString()
    if ($script:IsExe) {
        Show-GuiMessage -Text ("Unhandled error:`n" + $msg + "`n`nSee uninstall-exe.log for details.") -Icon "Error" | Out-Null
    }
    Invoke-Exit 1
}

# =====================================================================
# GUI / Prompt Helpers
# =====================================================================

function Test-HostInteractive {
    try {
        if ($null -eq $host) { return $false }
        if ($null -eq $host.UI) { return $false }
        if ($null -eq $host.UI.RawUI) { return $false }
        if ([Console]::IsInputRedirected) { return $false }
        return $true
    }
    catch {
        return $false
    }
}

function Initialize-GuiPrompts {
    $script:UseGuiPrompts = $false
    if ($env:PROTOFLEET_GUI_PROMPTS -eq "1") {
        try {
            Add-Type -AssemblyName System.Windows.Forms | Out-Null
            Add-Type -AssemblyName System.Drawing | Out-Null
            $script:UseGuiPrompts = $true
        }
        catch {
            $script:UseGuiPrompts = $false
        }
        Write-Log ("GUI prompts forced by env: " + $script:UseGuiPrompts)
        return
    }

    if ($script:IsExe) {
        try {
            Add-Type -AssemblyName System.Windows.Forms | Out-Null
            Add-Type -AssemblyName System.Drawing | Out-Null
            $script:UseGuiPrompts = $true
        }
        catch {
            $script:UseGuiPrompts = $false
        }
    }
    Write-Log ("GUI prompts available: " + $script:UseGuiPrompts)
}

function Show-FrontDoorChoice {
    if ($script:UseGuiPrompts) {
        $form = New-Object System.Windows.Forms.Form
        $form.Text = "Proto Fleet Uninstaller"
        $form.Width = 680
        $form.Height = 300
        $form.StartPosition = "CenterScreen"
        $form.FormBorderStyle = "FixedDialog"
        $form.MaximizeBox = $false
        $form.MinimizeBox = $false

        $label = New-Object System.Windows.Forms.Label
        $label.Text = "This will remove Proto Fleet containers, images, and deployment files.`nChoose how to handle data volumes:"
        $label.Width = 640
        $label.Height = 70
        $label.Top = 20
        $label.Left = 20
        $form.Controls.Add($label)

        $btnCancel = New-Object System.Windows.Forms.Button
        $btnCancel.Text = "No - Cancel"
        $btnCancel.Width = 160
        $btnCancel.Top = 140
        $btnCancel.Left = 20
        $btnCancel.Add_Click({ $form.Tag = "cancel"; $form.Close() })
        $form.Controls.Add($btnCancel)

        $btnClean = New-Object System.Windows.Forms.Button
        $btnClean.Text = "Delete Everything"
        $btnClean.Width = 180
        $btnClean.Top = 140
        $btnClean.Left = 200
        $btnClean.Add_Click({ $form.Tag = "clean"; $form.Close() })
        $form.Controls.Add($btnClean)

        $btnRetain = New-Object System.Windows.Forms.Button
        $btnRetain.Text = "Retain Data (Recommended)"
        $btnRetain.Width = 260
        $btnRetain.Top = 140
        $btnRetain.Left = 400
        $btnRetain.Add_Click({ $form.Tag = "retain"; $form.Close() })
        $form.Controls.Add($btnRetain)

        $form.ActiveControl = $btnRetain
        [void]$form.ShowDialog()
        return $form.Tag
    }

    if (-not (Test-HostInteractive)) {
        Write-ErrorMsg "No interactive console available. Re-run with -Silent -RetainData or -Silent -Clean."
        Invoke-Exit 1
    }

    Write-Block @(
        "Proto Fleet Uninstaller",
        "",
        "This will remove Proto Fleet containers, images, and deployment files.",
        "Choose how to handle data volumes:",
        "1) No - Cancel",
        "2) Delete everything (clean uninstall)",
        "3) Retain data (recommended)"
    )
    $choice = Read-Host "Enter choice [3]"
    if ([string]::IsNullOrWhiteSpace($choice)) { $choice = "3" }
    switch ($choice.Trim()) {
        "1" { return "cancel" }
        "2" { return "clean" }
        "3" { return "retain" }
        default { return "retain" }
    }
}

function Show-InputDialog {
    param(
        [string]$Prompt,
        [string]$Default = ""
    )

    if (-not $script:UseGuiPrompts) {
        if (-not (Test-HostInteractive)) {
            Write-ErrorMsg "No interactive console available. Re-run with -Silent -RetainData or -Silent -Clean."
            Invoke-Exit 1
        }
        $resp = Read-Host $Prompt
        if ([string]::IsNullOrWhiteSpace($resp)) { return $Default }
        return $resp
    }

    $form = New-Object System.Windows.Forms.Form
    $form.Text = "Proto Fleet Uninstaller"
    $form.Width = 560
    $form.Height = 200
    $form.StartPosition = "CenterScreen"
    $form.FormBorderStyle = "FixedDialog"
    $form.MaximizeBox = $false
    $form.MinimizeBox = $false

    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Prompt
    $label.Width = 520
    $label.Height = 40
    $label.Top = 15
    $label.Left = 20
    $form.Controls.Add($label)

    $textbox = New-Object System.Windows.Forms.TextBox
    $textbox.Width = 500
    $textbox.Top = 60
    $textbox.Left = 20
    $textbox.Text = $Default
    $form.Controls.Add($textbox)

    $btnOk = New-Object System.Windows.Forms.Button
    $btnOk.Text = "OK"
    $btnOk.Width = 80
    $btnOk.Top = 100
    $btnOk.Left = 360
    $btnOk.Add_Click({ $form.Tag = "ok"; $form.Close() })
    $form.Controls.Add($btnOk)

    $btnCancel = New-Object System.Windows.Forms.Button
    $btnCancel.Text = "Cancel"
    $btnCancel.Width = 80
    $btnCancel.Top = 100
    $btnCancel.Left = 440
    $btnCancel.Add_Click({ $form.Tag = "cancel"; $form.Close() })
    $form.Controls.Add($btnCancel)

    $form.ActiveControl = $textbox
    [void]$form.ShowDialog()
    if ($form.Tag -ne "ok") { return $null }
    return $textbox.Text
}

# =====================================================================
# Utility Helpers
# =====================================================================

function Escape-BashSingleQuotes {
    param([string]$Text)
    if ($null -eq $Text) { return "" }
    $sq = [char]39
    $dq = [char]34
    $replacement = "$sq$dq$sq$dq$sq"
    return ($Text -replace $sq, $replacement)
}

function Invoke-WslUserCapture {
    param([string]$Command)
    $wslArgs = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $wslArgs += "-d"
        $wslArgs += $script:WslDistro
    }
    $wslArgs += "bash"
    $wslArgs += "-lc"
    $wslArgs += $Command
    $out = & wsl.exe @wslArgs 2>&1
    return ($out | Out-String).Trim()
}

function Invoke-WslUserNoThrow {
    param([string]$Command)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $wslArgs = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $wslArgs += "-d"
            $wslArgs += $script:WslDistro
        }
        $wslArgs += "bash"
        $wslArgs += "-lc"
        $wslArgs += $Command
        $out = & wsl.exe @wslArgs 2>&1
        return ($out | Out-String).Trim()
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Invoke-WslUser {
    param([string]$Command)
    $wslArgs = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $wslArgs += "-d"
        $wslArgs += $script:WslDistro
    }
    $wslArgs += "bash"
    $wslArgs += "-lc"
    $wslArgs += $Command
    & wsl.exe @wslArgs | Out-Null
}

function Normalize-WslSpacing {
    param([string]$Value)
    if ([string]::IsNullOrWhiteSpace($Value)) {
        return $Value
    }

    $cleaned = $Value.Replace([char]0, ' ').Trim()
    if ([string]::IsNullOrWhiteSpace($cleaned)) {
        return $cleaned
    }

    $cleaned = [regex]::Replace(
        $cleaned,
        '\b(?:[A-Za-z0-9]\s+){3,}[A-Za-z0-9]\b',
        { param($m) ($m.Value -replace '\s+', '') })
    $cleaned = [regex]::Replace($cleaned, '\b(?:\d\s+){1,}\d\b', { param($m) ($m.Value -replace '\s+', '') })
    $cleaned = [regex]::Replace($cleaned, '\s*-\s*', '-')
    $cleaned = [regex]::Replace($cleaned, '(?<=\d)\s*\.\s*(?=\d)', '.')
    $cleaned = [regex]::Replace($cleaned, '\s{2,}', ' ').Trim()
    return $cleaned
}

function Get-InstalledWslDistros {
    $list = & wsl.exe -l -q 2>&1
    if ($LASTEXITCODE -ne 0) {
        return $null
    }

    $distros = @()
    foreach ($line in ($list -split "`n")) {
        $trimmedRaw = $line.Replace([char]0, ' ').Trim()
        $trimmed = Normalize-WslSpacing -Value $trimmedRaw
        if ([string]::IsNullOrWhiteSpace($trimmed)) {
            continue
        }

        $isDefault = $trimmedRaw.StartsWith("*")
        $name = Normalize-WslSpacing -Value ($trimmedRaw.TrimStart('*').Trim())
        if ([string]::IsNullOrWhiteSpace($name)) {
            continue
        }

        $distros += [PSCustomObject]@{
            Name = $name
            IsDefault = $isDefault
        }
    }

    return $distros
}

function Get-DefaultWslDistroName {
    $list = & wsl.exe -l -v 2>&1
    if ($LASTEXITCODE -ne 0) {
        return $null
    }

    foreach ($line in ($list -split "`n")) {
        $trimmed = $line.TrimStart()
        if (-not $trimmed.StartsWith("*")) {
            continue
        }

        $raw = $trimmed.Substring(1).Replace([char]0, ' ').Trim()
        if ([string]::IsNullOrWhiteSpace($raw)) {
            continue
        }

        $normalizedRaw = Normalize-WslSpacing -Value $raw
        $normalizedRaw = [regex]::Replace($normalizedRaw, '\s+(Running|Stopped|Installing)\s+\d+$', '')
        if (-not [string]::IsNullOrWhiteSpace($normalizedRaw)) {
            return $normalizedRaw
        }

        $parts = $raw -split "\s{2,}"
        if ($parts.Count -gt 0 -and -not [string]::IsNullOrWhiteSpace($parts[0])) {
            return (Normalize-WslSpacing -Value $parts[0].Trim())
        }

        return (Normalize-WslSpacing -Value (($raw -split "\s+")[0]))
    }

    return $null
}

function Ensure-WslDistro {
    $explicitDistro = $null
    if (-not [string]::IsNullOrWhiteSpace($WslDistro)) {
        $explicitDistro = Normalize-WslSpacing -Value $WslDistro.Trim()
    }

    $distros = Get-InstalledWslDistros
    if ($null -eq $distros) {
        Write-ErrorMsg "WSL is not available. Ensure WSL is installed and try again."
        Invoke-Exit 1
    }

    if ($distros.Count -eq 0) {
        Write-ErrorMsg "No WSL distributions are installed. Install Ubuntu (or another distro) and retry."
        Invoke-Exit 1
    }

    if (-not [string]::IsNullOrWhiteSpace($explicitDistro)) {
        $explicitMatch = $distros | Where-Object {
            [string]::Equals($_.Name, $explicitDistro, [System.StringComparison]::OrdinalIgnoreCase)
        } | Select-Object -First 1
        if ($null -eq $explicitMatch) {
            Write-ErrorMsg "WSL distribution '$explicitDistro' not found. Re-run with -WslDistro."
            Invoke-Exit 1
        }

        $script:WslDistro = $explicitMatch.Name
        return
    }

    $selected = $distros | Where-Object { $_.IsDefault } | Select-Object -First 1
    if ($null -eq $selected) {
        $defaultName = Get-DefaultWslDistroName
        if (-not [string]::IsNullOrWhiteSpace($defaultName)) {
            $selected = $distros | Where-Object {
                [string]::Equals($_.Name, $defaultName, [System.StringComparison]::OrdinalIgnoreCase)
            } | Select-Object -First 1
        }
    }

    if ($null -eq $selected) {
        $selected = $distros | Where-Object {
            $_.Name.StartsWith("Ubuntu", [System.StringComparison]::OrdinalIgnoreCase)
        } | Select-Object -First 1
    }

    if ($null -eq $selected) {
        $selected = $distros | Select-Object -First 1
    }

    $script:WslDistro = $selected.Name
    Write-Host ("Using WSL distro: " + $script:WslDistro)
    Write-Log ("Auto-selected WSL distro: " + $script:WslDistro)
}

function Get-FirstAbsoluteWslPathLine {
    param([string]$Output)
    if ([string]::IsNullOrWhiteSpace($Output)) {
        return $null
    }

    foreach ($line in ($Output -split "`n")) {
        $candidate = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($candidate)) {
            continue
        }
        if ($candidate.StartsWith("/")) {
            return $candidate
        }
    }

    return $null
}

function Get-WslHomeDir {
    $homeFromEnv = Invoke-WslUserCapture 'printf "%s" "$HOME"'
    if ($LASTEXITCODE -eq 0) {
        $resolved = Get-FirstAbsoluteWslPathLine -Output $homeFromEnv
        if (-not [string]::IsNullOrWhiteSpace($resolved)) {
            return $resolved.TrimEnd("/")
        }
    }

    $homeFromId = Invoke-WslUserCapture 'getent passwd "$(id -u)" | cut -d: -f6'
    if ($LASTEXITCODE -eq 0) {
        $resolved = Get-FirstAbsoluteWslPathLine -Output $homeFromId
        if (-not [string]::IsNullOrWhiteSpace($resolved)) {
            return $resolved.TrimEnd("/")
        }
    }

    Write-Log "Could not resolve WSL user home directory reliably; falling back to /home."
    return "/home"
}

function Resolve-WslPath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    if ($Path.StartsWith("~")) {
        $wslHome = Get-WslHomeDir
        if ($Path -eq "~") { return $wslHome }
        return ($wslHome.TrimEnd("/") + $Path.Substring(1))
    }
    return $Path
}

function ConvertTo-WSLPath {
    param([string]$WindowsPath)
    if ([string]::IsNullOrWhiteSpace($WindowsPath)) { return $WindowsPath }
    $wslArgs = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $wslArgs += "-d"
        $wslArgs += $script:WslDistro
    }
    $wslArgs += "wslpath"
    $wslArgs += "-u"
    $wslArgs += $WindowsPath
    $out = & wsl.exe @wslArgs 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    return ($out | Out-String).Trim()
}

function Test-AllowedProtoFleetPath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }
    $normalized = $Path.Trim().TrimEnd("/")
    if ([string]::IsNullOrWhiteSpace($normalized)) { $normalized = "/" }
    if ($normalized -eq "/" -or $normalized -eq "/home" -or $normalized -match '^/home/[^/]+$') {
        return $false
    }
    return ($normalized -match '^/home/[^/]+/proto-fleet(?:/deployment)?$')
}

function Assert-SafeRemovalPath {
    param([string]$Path)

    if (-not (Test-AllowedProtoFleetPath -Path $Path)) {
        throw ("Unsafe deployment path resolved: '" + $Path + "'. Expected /home/<user>/proto-fleet or /home/<user>/proto-fleet/deployment.")
    }
}

function Test-DeploymentRoot {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }
    if (-not (Test-AllowedProtoFleetPath -Path $Path)) { return $false }
    $p = Escape-BashSingleQuotes $Path
    $cmd = "[ -f '$p/docker-compose.yaml' ] && [ -d '$p/server' ] && [ -d '$p/client' ]"
    Invoke-WslUserNoThrow $cmd | Out-Null
    return ($LASTEXITCODE -eq 0)
}

function Add-UniquePath {
    param(
        [System.Collections.Generic.List[string]]$Items,
        [string]$Value
    )
    if ([string]::IsNullOrWhiteSpace($Value)) {
        return
    }

    $candidate = $Value.Trim().TrimEnd("/")
    foreach ($existing in $Items) {
        if ([string]::Equals($existing, $candidate, [System.StringComparison]::Ordinal)) {
            return
        }
    }
    $Items.Add($candidate) | Out-Null
}

function Normalize-WslCandidatePath {
    param(
        [string]$PathValue,
        [string]$SourceLabel
    )

    if ([string]::IsNullOrWhiteSpace($PathValue)) {
        return $null
    }

    $normalizedInput = $PathValue.Trim()
    if ([string]::IsNullOrWhiteSpace($normalizedInput)) {
        return $null
    }

    $wslPath = $null
    if ($normalizedInput.StartsWith("~") -or $normalizedInput.StartsWith("/")) {
        $wslPath = Resolve-WslPath -Path $normalizedInput
    }
    elseif (Test-Path -LiteralPath $normalizedInput) {
        $converted = ConvertTo-WSLPath -WindowsPath $normalizedInput
        if ([string]::IsNullOrWhiteSpace($converted)) {
            Write-Log ("Could not convert Windows path to WSL path for source [" + $SourceLabel + "].")
            return $null
        }
        $wslPath = Resolve-WslPath -Path $converted
    }
    else {
        Write-Log ("Source [" + $SourceLabel + "] is neither a WSL-style path nor an existing Windows path.")
        return $null
    }

    if ([string]::IsNullOrWhiteSpace($wslPath)) {
        Write-Log ("Source [" + $SourceLabel + "] resolved to an empty WSL path.")
        return $null
    }

    $wslPath = $wslPath.Trim()
    if (-not $wslPath.StartsWith("/")) {
        Write-Log ("Source [" + $SourceLabel + "] resolved to non-absolute WSL path: " + $wslPath)
        return $null
    }

    $wslPath = $wslPath.TrimEnd("/")
    if ([string]::IsNullOrWhiteSpace($wslPath)) {
        $wslPath = "/"
    }

    return $wslPath
}

function Resolve-DeploymentCandidateNoTraversal {
    param(
        [string]$WslPath,
        [string]$SourceLabel
    )

    if ([string]::IsNullOrWhiteSpace($WslPath)) {
        return $null
    }

    $candidates = New-Object System.Collections.Generic.List[string]
    if ($WslPath.EndsWith("/deployment")) {
        Add-UniquePath -Items $candidates -Value $WslPath
    }
    else {
        Add-UniquePath -Items $candidates -Value ($WslPath + "/deployment")
        Add-UniquePath -Items $candidates -Value $WslPath
    }

    foreach ($candidate in $candidates) {
        if (-not (Test-AllowedProtoFleetPath -Path $candidate)) {
            Write-Log ("Rejected candidate from [" + $SourceLabel + "] (outside allowed path): " + $candidate)
            continue
        }

        if (Test-DeploymentRoot -Path $candidate) {
            Write-Log ("Accepted deployment candidate from [" + $SourceLabel + "]: " + $candidate)
            return $candidate
        }

        Write-Log ("Rejected candidate from [" + $SourceLabel + "] (missing deployment markers): " + $candidate)
    }

    return $null
}

function Resolve-DeploymentFromPathInput {
    param(
        [string]$PathValue,
        [string]$SourceLabel
    )

    if ([string]::IsNullOrWhiteSpace($PathValue)) {
        return $null
    }

    Write-Log ("Trying deployment source [" + $SourceLabel + "]: " + $PathValue)

    $wslPath = Normalize-WslCandidatePath -PathValue $PathValue -SourceLabel $SourceLabel
    if ([string]::IsNullOrWhiteSpace($wslPath)) {
        Write-Log ("Deployment source [" + $SourceLabel + "] did not resolve to a usable WSL path.")
        return $null
    }

    Write-Log ("Normalized source [" + $SourceLabel + "] to WSL path: " + $wslPath)
    $resolved = Resolve-DeploymentCandidateNoTraversal -WslPath $wslPath -SourceLabel $SourceLabel

    if (-not [string]::IsNullOrWhiteSpace($resolved)) {
        Write-Log ("Resolved deployment source [" + $SourceLabel + "] to: " + $resolved)
    }
    else {
        Write-Log ("Deployment source [" + $SourceLabel + "] did not resolve to a valid root.")
    }

    return $resolved
}

function Resolve-DefaultDeploymentPath {
    $wslHomeDir = Get-WslHomeDir
    if ([string]::IsNullOrWhiteSpace($wslHomeDir) -or $wslHomeDir -eq "/home") {
        Write-Log "Could not determine user-specific WSL home; default deployment lookup skipped."
        return $null
    }

    $defaultRoot = $wslHomeDir.TrimEnd("/") + "/proto-fleet"
    Write-Log ("Trying default Proto Fleet root: " + $defaultRoot)
    return (Resolve-DeploymentCandidateNoTraversal -WslPath $defaultRoot -SourceLabel "DefaultInstallDir")
}

function Resolve-DeploymentPath {
    if (-not [string]::IsNullOrWhiteSpace($DeploymentPath)) {
        $resolvedFromDeploymentPath = Resolve-DeploymentFromPathInput -PathValue $DeploymentPath -SourceLabel "DeploymentPath"
        if (-not [string]::IsNullOrWhiteSpace($resolvedFromDeploymentPath)) {
            return $resolvedFromDeploymentPath
        }

        Write-ErrorMsg ("Provided -DeploymentPath is invalid or outside allowed location: " + $DeploymentPath)
        Invoke-Exit 1
    }

    $hasCustomInstallDir = (-not [string]::IsNullOrWhiteSpace($InstallDir)) -and `
        (-not [string]::Equals($InstallDir.Trim(), "~/proto-fleet", [System.StringComparison]::Ordinal))

    if ($hasCustomInstallDir) {
        $resolvedFromInstallDir = Resolve-DeploymentFromPathInput -PathValue $InstallDir -SourceLabel "InstallDir"
        if (-not [string]::IsNullOrWhiteSpace($resolvedFromInstallDir)) {
            return $resolvedFromInstallDir
        }

        Write-ErrorMsg ("Provided -InstallDir is invalid or outside allowed location: " + $InstallDir)
        Invoke-Exit 1
    }

    return (Resolve-DefaultDeploymentPath)
}

function Is-WindowsMount {
    param([string]$WslPath)
    if ([string]::IsNullOrWhiteSpace($WslPath)) { return $false }
    if ($WslPath -match "^/mnt/[a-zA-Z]/") { return $true }
    $p = Escape-BashSingleQuotes $WslPath
    $out = Invoke-WslUserNoThrow "wslpath -m '$p' 2>/dev/null || true"
    if ($out -match "^[A-Za-z]:\\") { return $true }
    return $false
}

function Ensure-DockerRunning {
    $out = Invoke-WslUserNoThrow "docker info >/dev/null 2>&1"
    Write-Log ("Docker info exit code: " + $LASTEXITCODE)
    return ($LASTEXITCODE -eq 0)
}

function Get-InstallRootPath {
    param([string]$ResolvedDeploymentPath)

    if ([string]::IsNullOrWhiteSpace($ResolvedDeploymentPath)) {
        return $null
    }

    $normalized = $ResolvedDeploymentPath.Trim().TrimEnd("/")
    if ($normalized.EndsWith("/deployment")) {
        return $normalized.Substring(0, $normalized.Length - "/deployment".Length)
    }
    return $normalized
}

function Remove-ProtoFleetSystemdArtifacts {
    $cleanupCmd = @'
if command -v systemctl >/dev/null 2>&1; then
  units="$(systemctl --user list-unit-files --type=service --no-legend 2>/dev/null | awk '{print $1}' | grep -E '^(protofleet|proto-fleet|fleet).*\.service$' || true)"
  if [ -n "$units" ]; then
    printf "%s\n" "$units" | while IFS= read -r unit; do
      [ -z "$unit" ] && continue
      systemctl --user disable --now "$unit" >/dev/null 2>&1 || true
    done
  fi
  systemctl --user daemon-reload >/dev/null 2>&1 || true
  systemctl --user reset-failed >/dev/null 2>&1 || true
fi
rm -f ~/.config/systemd/user/protofleet*.service ~/.config/systemd/user/proto-fleet*.service ~/.config/systemd/user/fleet*.service 2>/dev/null || true
'@
    Invoke-WslUserNoThrow $cleanupCmd | Out-Null
}

function Invoke-Action {
    param(
        [string]$Description,
        [scriptblock]$Action
    )
    if ($WhatIf) {
        Write-Host ("[WHATIF] " + $Description)
        return
    }
    & $Action
}

# =====================================================================
# Main
# =====================================================================

$exePath = $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($exePath)) {
    try {
        $exePath = [System.Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
    }
    catch {
        $exePath = ""
    }
}
if (-not [string]::IsNullOrWhiteSpace($exePath)) {
    $script:IsExe = $exePath.ToLower().EndsWith(".exe")
}
Write-Log ("ExePath: " + $exePath)
Write-Log ("IsExe: " + $script:IsExe)

if ($script:IsExe) {
    $logDir = Split-Path -Parent $exePath
    if ([string]::IsNullOrWhiteSpace($logDir)) {
        $logDir = (Get-Location).Path
    }
    $logPath = Join-Path $logDir "uninstall-exe.log"
    $script:LogPath = Join-Path $env:TEMP ("protofleet-uninstall-debug-" + $PID + ".log")
    Write-Log "Uninstaller starting"
    try {
        Start-Transcript -Path $logPath -Append | Out-Null
        Write-Host "Logging to: $logPath"
        Write-Host "Debug log: $($script:LogPath)"
        $script:TranscriptStarted = $true
    }
    catch {
        # ignore
    }
}

Initialize-GuiPrompts

if ($Silent) {
    if (-not $RetainData -and -not $Clean) {
        Write-ErrorMsg "Silent mode requires -RetainData or -Clean."
        Invoke-Exit 1
    }
}
else {
    if (-not $RetainData -and -not $Clean) {
        $choice = Show-FrontDoorChoice
        if ($choice -eq "cancel") {
            Write-Host "Uninstall canceled."
            Invoke-Exit 0
        }
        elseif ($choice -eq "clean") {
            $Clean = $true
        }
        else {
            $RetainData = $true
        }
    }
}

Ensure-WslDistro

# Resolve deployment path
$resolvedDeployment = Resolve-DeploymentPath

if (-not $resolvedDeployment) {
    if ($Silent) {
        Write-ErrorMsg "Deployment path not found. Provide -DeploymentPath or -InstallDir."
        Invoke-Exit 1
    }
    $prompt = "Enter deployment path (/home/<user>/proto-fleet or /home/<user>/proto-fleet/deployment):"
    $inputPath = Show-InputDialog -Prompt $prompt -Default ""
    if ([string]::IsNullOrWhiteSpace($inputPath)) {
        Write-ErrorMsg "Deployment path not provided."
        Invoke-Exit 1
    }
    $resolvedDeployment = Resolve-DeploymentFromPathInput -PathValue $inputPath -SourceLabel "UserPrompt"
    if (-not $resolvedDeployment) {
        Write-ErrorMsg "Invalid deployment path. Expected /home/<user>/proto-fleet or /home/<user>/proto-fleet/deployment."
        Invoke-Exit 1
    }
}

if (-not $resolvedDeployment) {
    Write-ErrorMsg "Could not locate a valid Proto Fleet deployment."
    Invoke-Exit 1
}

$deploymentPath = $resolvedDeployment
$installRootPath = Get-InstallRootPath -ResolvedDeploymentPath $deploymentPath
Write-Log ("Final resolved deployment path: " + $deploymentPath)
Write-Log ("Final resolved install root: " + $installRootPath)
Assert-SafeRemovalPath -Path $deploymentPath
Assert-SafeRemovalPath -Path $installRootPath

Write-Step "Preparing to uninstall Proto Fleet..."
Write-Host "WSL distro: $script:WslDistro"
Write-Host "Deployment path: $deploymentPath"
Write-Host "Install root: $installRootPath"

if (-not (Ensure-DockerRunning)) {
    Write-ErrorMsg "Docker is not accessible for the current WSL user. Start Docker in WSL and retry."
    Invoke-Exit 1
}

Invoke-Action "Tear down Proto Fleet containers" {
    $cmd = "cd '$(Escape-BashSingleQuotes $deploymentPath)' && docker compose -f docker-compose.yaml down --remove-orphans >/dev/null 2>&1 || true"
    Invoke-WslUserNoThrow $cmd | Out-Null
}

Invoke-Action "Delete Proto Fleet images" {
    $cmd = "cd '$(Escape-BashSingleQuotes $deploymentPath)' && docker compose -f docker-compose.yaml down --rmi all >/dev/null 2>&1 || true"
    Invoke-WslUserNoThrow $cmd | Out-Null
}

Invoke-Action "Remove Proto Fleet systemd user artifacts" {
    Remove-ProtoFleetSystemdArtifacts
}

$isWindowsMount = Is-WindowsMount -WslPath $deploymentPath
if (-not $isWindowsMount) {
    Invoke-Action "Delete Proto Fleet deploy directory from WSL user home" {
        Invoke-WslUserNoThrow "rm -rf '$(Escape-BashSingleQuotes $installRootPath)' >/dev/null 2>&1 || true" | Out-Null
    }
}
else {
    Write-WarningMsg "Deployment path resolved to a Windows mount; skipping delete for safety."
}

Invoke-Action "Remove WSL temp artifacts" {
    Invoke-WslUserNoThrow "rm -f /tmp/proto-fleet-*.tar.gz /tmp/proto-fleet-deployment.tar.gz /tmp/pf-docker-install.log 2>/dev/null || true" | Out-Null
}

if ($Clean) {
    $projectName = Invoke-WslUserNoThrow "basename '$(Escape-BashSingleQuotes $deploymentPath)'"
    if (-not [string]::IsNullOrWhiteSpace($projectName)) {
        $projectName = $projectName.Trim()
        $volListCmd = "docker volume ls -q | grep -E '^${projectName}[-_]timescaledb-data$|^${projectName}[-_](mysql|influxdb)$' || true"
        $vols = Invoke-WslUserNoThrow $volListCmd
        foreach ($vol in ($vols -split "`n")) {
            $v = $vol.Trim()
            if (-not [string]::IsNullOrWhiteSpace($v)) {
                Invoke-Action ("Remove volume " + $v) {
                    Invoke-WslUserNoThrow "docker volume rm '$v' >/dev/null 2>&1 || true" | Out-Null
                }
            }
        }
    }
}

Invoke-Action "Remove scheduled task ProtoFleet-StartWSL" {
    try {
        $task = Get-ScheduledTask -TaskName "ProtoFleet-StartWSL" -ErrorAction SilentlyContinue
        if ($null -ne $task) {
            Unregister-ScheduledTask -TaskName "ProtoFleet-StartWSL" -Confirm:$false | Out-Null
        }
        else {
            Start-Process -FilePath "schtasks.exe" -ArgumentList @("/Delete", "/TN", "ProtoFleet-StartWSL", "/F") -Wait -WindowStyle Hidden
        }
    }
    catch {
        # ignore
    }
}

Invoke-Action "Remove installer RunOnce resume entry" {
    try {
        Remove-ItemProperty `
            -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\RunOnce" `
            -Name "ProtoFleetInstallerResume" `
            -ErrorAction SilentlyContinue
    }
    catch {
        # ignore
    }
}

Invoke-Action "Remove installer resume state file" {
    try {
        $programData = [Environment]::GetFolderPath([Environment+SpecialFolder]::CommonApplicationData)
        $resumeStatePath = Join-Path $programData "ProtoFleet\resume-state.json"
        if (Test-Path -LiteralPath $resumeStatePath) {
            Remove-Item -LiteralPath $resumeStatePath -Force -ErrorAction SilentlyContinue
        }
    }
    catch {
        # ignore
    }
}

Write-Block @(
    "Uninstall complete.",
    ("Mode: " + $(if ($Clean) { "Clean (data removed)" } else { "Retain data" })),
    ("Deployment removed: " + $(if ($isWindowsMount) { "No (Windows mount)" } else { "Yes" })),
    ("Volumes removed: " + $(if ($Clean) { "Yes" } else { "No" }))
)
