# Proto Fleet - Windows Installer (WSL/Docker + Fleet)
<#
.SYNOPSIS
    Installs WSL2, Docker Engine, and Proto Fleet on Windows.

.DESCRIPTION
    Single self-contained installer intended for ps2exe. Uses GUI prompts when
    interactive and console fallbacks when not. Requires local tarball only.

.PARAMETER Version
    Optional label for the version being installed (no downloads performed).

.PARAMETER TarballPath
    Path to local proto-fleet-*.tar.gz tarball.

.PARAMETER DeploymentPath
    Path to an extracted Proto Fleet deployment (root or any subfolder).

.PARAMETER ConfigFile
    Path to .env config file.

.PARAMETER InstallDir
    Installation directory in WSL (default: ~/proto-fleet).

.PARAMETER Force
    Skip confirmation prompts where possible.

.PARAMETER Guided
    Enable guided setup prompts for configuration.

.PARAMETER Silent
    Non-interactive mode: show progress/errors only; fail if required inputs missing.
#>

[CmdletBinding()]
param(
    [string]$Version = "latest",
    [string]$TarballPath = "",
    [string]$DeploymentPath = "",
    [string]$ConfigFile = "",
    [string]$InstallDir = "~/proto-fleet",
    [switch]$Force,
    [switch]$Guided,
    [switch]$Silent
)

$ErrorActionPreference = "Stop"

# Keep output predictable in PS5/EXE and avoid mojibake from WSL tools
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$OutputEncoding = [Console]::OutputEncoding

# Constants
$DEPLOYMENT_DIR = "deployment"
$REQUIRED_PLUGINS = @("proto-plugin-amd64", "proto-plugin-arm64", "antminer-plugin-amd64", "antminer-plugin-arm64")
$MIN_RAM_GB = 8
$MIN_DISK_GB = 20
$MIN_WIN10_BUILD = 19041

$script:UseGuiPrompts = $false
$script:WslDistro = ""
$script:SimpleSetup = $false
$script:IsExe = $false
$script:TranscriptStarted = $false
$script:CachedWslStatusText = $null
$script:CachedWslStatusExitCode = $null
$script:SpinnerFrames = @('|', '/', '-', '\')
$global:ProtoFleetDidExitPause = $false

# ============================================================================
# Output Helpers
# ============================================================================

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

function Reset-ConsoleLine {
    [Console]::Write("`r")
    Write-Host ""
}

function Ensure-SpinnerType {
    if ("ProtoFleet.ConsoleSpinner" -as [type]) { return }

    $typeDef = @"
using System;
using System.Threading;

namespace ProtoFleet {
    public static class ConsoleSpinner {
        private static Timer _timer;
        private static string _activity = "";
        private static int _idx = 0;
        private static int _lastLen = 0;
        private static readonly string[] _frames = new string[] { "|", "/", "-", "\\" };
        private static DateTime _start;

        public static void Start(string activity) {
            Stop();
            _activity = activity ?? "";
            _start = DateTime.UtcNow;
            _idx = 0;
            _timer = new Timer(_ => {
                try {
                    int elapsed = (int)(DateTime.UtcNow - _start).TotalSeconds;
                    string spin = _frames[_idx++ % _frames.Length];
                    string text = string.Format("{0} {1}  Elapsed {2}s", _activity, spin, elapsed);
                    try {
                        Console.SetCursorPosition(0, Console.CursorTop);
                    } catch { }
                    Console.Write(text);
                    if (text.Length < _lastLen) {
                        Console.Write(new string(' ', _lastLen - text.Length));
                    }
                    _lastLen = text.Length;
                } catch { }
            }, null, 0, 200);
        }

        public static void Stop() {
            if (_timer != null) {
                try { _timer.Dispose(); } catch { }
                _timer = null;
            }
            try {
                if (!string.IsNullOrEmpty(_activity)) {
                    try {
                        Console.SetCursorPosition(0, Console.CursorTop);
                    } catch { }
                    Console.Write(new string(' ', Math.Max(_lastLen, 0)));
                    try {
                        Console.SetCursorPosition(0, Console.CursorTop);
                    } catch { }
                    Console.WriteLine("");
                }
            } catch { }
            _activity = "";
            _lastLen = 0;
        }
    }
}
"@
    Add-Type -TypeDefinition $typeDef -ErrorAction SilentlyContinue | Out-Null
}

function Start-Spinner {
    param([string]$Activity)

    if ($env:PROTOFLEET_NO_SPINNER) { return }

    Ensure-SpinnerType
    [ProtoFleet.ConsoleSpinner]::Start($Activity)
}

function Stop-Spinner {
    if ($env:PROTOFLEET_NO_SPINNER) { return }
    Ensure-SpinnerType
    [ProtoFleet.ConsoleSpinner]::Stop()
}

# ============================================================================
# UI / Prompt Helpers
# ============================================================================

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
        return
    }

    $path = $MyInvocation.MyCommand.Path
    if (-not [string]::IsNullOrWhiteSpace($path) -and $path.ToLower().EndsWith(".exe")) {
        try {
            Add-Type -AssemblyName System.Windows.Forms | Out-Null
            Add-Type -AssemblyName System.Drawing | Out-Null
            $script:UseGuiPrompts = $true
        }
        catch {
            $script:UseGuiPrompts = $false
        }
    }
}

function Show-YesNoDialog {
    param(
        [string]$Prompt,
        [bool]$DefaultYes = $true
    )

    $caption = "Proto Fleet Installer"
    $buttons = [System.Windows.Forms.MessageBoxButtons]::YesNo
    $icon = [System.Windows.Forms.MessageBoxIcon]::Question
    $defaultButton = if ($DefaultYes) {
        [System.Windows.Forms.MessageBoxDefaultButton]::Button1
    }
    else {
        [System.Windows.Forms.MessageBoxDefaultButton]::Button2
    }

    $result = [System.Windows.Forms.MessageBox]::Show($Prompt, $caption, $buttons, $icon, $defaultButton)
    return ($result -eq [System.Windows.Forms.DialogResult]::Yes)
}

function Show-InputDialog {
    param(
        [string]$Prompt,
        [string]$DefaultValue = ""
    )

    $form = New-Object System.Windows.Forms.Form
    $form.Text = "Proto Fleet Installer"
    $form.StartPosition = "CenterScreen"
    $form.Width = 520
    $form.Height = 170
    $form.FormBorderStyle = "FixedDialog"
    $form.MaximizeBox = $false
    $form.MinimizeBox = $false

    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Prompt
    $label.Left = 10
    $label.Top = 10
    $label.Width = 480
    $label.Height = 40
    $form.Controls.Add($label)

    $textBox = New-Object System.Windows.Forms.TextBox
    $textBox.Left = 10
    $textBox.Top = 60
    $textBox.Width = 480
    $textBox.Text = $DefaultValue
    $form.Controls.Add($textBox)

    $okButton = New-Object System.Windows.Forms.Button
    $okButton.Text = "OK"
    $okButton.Left = 330
    $okButton.Top = 95
    $okButton.Width = 75
    $okButton.Add_Click({ $form.DialogResult = [System.Windows.Forms.DialogResult]::OK })
    $form.Controls.Add($okButton)

    $cancelButton = New-Object System.Windows.Forms.Button
    $cancelButton.Text = "Cancel"
    $cancelButton.Left = 415
    $cancelButton.Top = 95
    $cancelButton.Width = 75
    $cancelButton.Add_Click({ $form.DialogResult = [System.Windows.Forms.DialogResult]::Cancel })
    $form.Controls.Add($cancelButton)

    $form.AcceptButton = $okButton
    $form.CancelButton = $cancelButton

    $result = $form.ShowDialog()
    if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
        return $textBox.Text
    }
    return ""
}

function Show-PasswordDialog {
    param([string]$Prompt)

    $form = New-Object System.Windows.Forms.Form
    $form.Text = "Proto Fleet Installer"
    $form.StartPosition = "CenterScreen"
    $form.Width = 520
    $form.Height = 170
    $form.FormBorderStyle = "FixedDialog"
    $form.MaximizeBox = $false
    $form.MinimizeBox = $false

    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Prompt
    $label.Left = 10
    $label.Top = 10
    $label.Width = 480
    $label.Height = 40
    $form.Controls.Add($label)

    $textBox = New-Object System.Windows.Forms.TextBox
    $textBox.Left = 10
    $textBox.Top = 60
    $textBox.Width = 480
    $textBox.UseSystemPasswordChar = $true
    $form.Controls.Add($textBox)

    $okButton = New-Object System.Windows.Forms.Button
    $okButton.Text = "OK"
    $okButton.Left = 330
    $okButton.Top = 95
    $okButton.Width = 75
    $okButton.Add_Click({ $form.DialogResult = [System.Windows.Forms.DialogResult]::OK })
    $form.Controls.Add($okButton)

    $cancelButton = New-Object System.Windows.Forms.Button
    $cancelButton.Text = "Cancel"
    $cancelButton.Left = 415
    $cancelButton.Top = 95
    $cancelButton.Width = 75
    $cancelButton.Add_Click({ $form.DialogResult = [System.Windows.Forms.DialogResult]::Cancel })
    $form.Controls.Add($cancelButton)

    $form.AcceptButton = $okButton
    $form.CancelButton = $cancelButton

    $result = $form.ShowDialog()
    if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
        return $textBox.Text
    }
    return ""
}

function Show-OpenFileDialog {
    param(
        [string]$Title,
        [string]$Filter
    )

    $dialog = New-Object System.Windows.Forms.OpenFileDialog
    $dialog.Title = $Title
    $dialog.Filter = $Filter
    $dialog.Multiselect = $false
    $dialog.RestoreDirectory = $true
    $result = $dialog.ShowDialog()
    if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
        return $dialog.FileName
    }
    return ""
}

function Read-HostLine {
    param([string]$Prompt)
    if ($script:UseGuiPrompts) {
        return (Show-InputDialog -Prompt $Prompt)
    }
    Write-Host ""
    return Read-Host $Prompt
}

function Read-YesNoPrompt {
    param(
        [string]$Prompt,
        [bool]$DefaultYes = $true
    )

    if ($script:UseGuiPrompts) {
        return (Show-YesNoDialog -Prompt $Prompt -DefaultYes:$DefaultYes)
    }

    $suffix = if ($DefaultYes) { "(Y/n)" } else { "(y/N)" }
    $answer = Read-HostLine "$Prompt $suffix"
    if ([string]::IsNullOrWhiteSpace($answer)) {
        return $DefaultYes
    }
    return ($answer -match "^[Yy]$")
}

function Read-SecureInput {
    param([string]$Prompt)

    if ($script:UseGuiPrompts) {
        return (Show-PasswordDialog -Prompt $Prompt)
    }

    $secureString = Read-Host -Prompt $Prompt -AsSecureString
    $bstr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureString)
    $plainText = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)

    return $plainText
}

function Show-ChoiceDialog {
    param(
        [string]$Prompt,
        [hashtable[]]$Options,
        [string]$DefaultValue = ""
    )

    $form = New-Object System.Windows.Forms.Form
    $form.Text = "Proto Fleet Installer"
    $form.StartPosition = "CenterScreen"
    $form.Width = 640
    $form.Height = 360
    $form.FormBorderStyle = "FixedDialog"
    $form.MaximizeBox = $false
    $form.MinimizeBox = $false

    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Prompt
    $label.Left = 10
    $label.Top = 10
    $label.Width = 600
    $label.Height = 30
    $form.Controls.Add($label)

    $details = New-Object System.Windows.Forms.TextBox
    $details.Left = 10
    $details.Top = 45
    $details.Width = 600
    $details.Height = 190
    $details.Multiline = $true
    $details.ReadOnly = $true
    $details.ScrollBars = "Vertical"
    $details.BackColor = [System.Drawing.SystemColors]::Window
    $details.TabStop = $false
    $details.Text = ($Options | ForEach-Object {
        ("- {0}`r`n  {1}" -f $_.Label, $_.Description)
    }) -join "`r`n`r`n"
    $form.Controls.Add($details)

    $buttonPanel = New-Object System.Windows.Forms.FlowLayoutPanel
    $buttonPanel.Left = 10
    $buttonPanel.Top = 245
    $buttonPanel.Width = 600
    $buttonPanel.Height = 60
    $buttonPanel.FlowDirection = "LeftToRight"
    $buttonPanel.WrapContents = $true
    $form.Controls.Add($buttonPanel)

    $script:ChoiceDialogValue = ""
    foreach ($opt in $Options) {
        $btn = New-Object System.Windows.Forms.Button
        $btn.Text = $opt.Label
        $btn.Width = 180
        $btn.Height = 32
        $btn.Tag = $opt.Value
        if ($opt.Value -eq $DefaultValue) {
            $form.AcceptButton = $btn
        }
        $btn.Add_Click({
            param($sender, $eventArgs)
            $script:ChoiceDialogValue = $sender.Tag
            $form.DialogResult = [System.Windows.Forms.DialogResult]::OK
            $form.Close()
        })
        $buttonPanel.Controls.Add($btn)
    }

    $cancelButton = New-Object System.Windows.Forms.Button
    $cancelButton.Text = "Cancel"
    $cancelButton.Width = 100
    $cancelButton.Height = 32
    $cancelButton.Add_Click({ $form.DialogResult = [System.Windows.Forms.DialogResult]::Cancel })
    $buttonPanel.Controls.Add($cancelButton)

    $form.CancelButton = $cancelButton

    $result = $form.ShowDialog()
    if ($result -eq [System.Windows.Forms.DialogResult]::OK -and -not [string]::IsNullOrWhiteSpace($script:ChoiceDialogValue)) {
        if ($env:PROTOFLEET_DEBUG -eq "1") {
            Write-Host ("DEBUG: Choice dialog selected value: {0}" -f $script:ChoiceDialogValue)
        }
        return $script:ChoiceDialogValue
    }
    return ""
}

function Read-ChoicePrompt {
    param(
        [string]$Prompt,
        [hashtable[]]$Options,
        [string]$DefaultValue
    )

    if ($script:UseGuiPrompts) {
        return (Show-ChoiceDialog -Prompt $Prompt -Options $Options -DefaultValue $DefaultValue)
    }

    Write-Host ""
    Write-Host $Prompt
    Write-Host ""
    foreach ($opt in $Options) {
        Write-Host ("- {0}" -f $opt.Label)
        Write-Host ("  {0}" -f $opt.Description)
        Write-Host ""
    }

    $accepted = @{}
    foreach ($opt in $Options) {
        $accepted[$opt.Value.ToLower()] = $opt.Value
        $accepted[$opt.Label.ToLower()] = $opt.Value
    }

    $defaultLabel = ($Options | Where-Object { $_.Value -eq $DefaultValue } | Select-Object -First 1).Label
    if ([string]::IsNullOrWhiteSpace($defaultLabel)) { $defaultLabel = $DefaultValue }

    while ($true) {
        $answer = Read-HostLine ("Type choice [{0}]" -f $defaultLabel)
        if ([string]::IsNullOrWhiteSpace($answer)) {
            return $DefaultValue
        }
        $key = $answer.Trim().ToLower()
        if ($accepted.ContainsKey($key)) {
            return $accepted[$key]
        }
        Write-WarningMsg "Invalid choice. Please type one of the option names shown above."
    }
}

function Show-CertDialog {
    param(
        [string]$Prompt,
        [string]$Details,
        [bool]$ShowOpenFolder
    )

    $form = New-Object System.Windows.Forms.Form
    $form.Text = "Proto Fleet Installer"
    $form.StartPosition = "CenterScreen"
    $form.Width = 680
    $form.Height = 420
    $form.FormBorderStyle = "FixedDialog"
    $form.MaximizeBox = $false
    $form.MinimizeBox = $false

    $label = New-Object System.Windows.Forms.Label
    $label.Text = $Prompt
    $label.Left = 10
    $label.Top = 10
    $label.Width = 640
    $label.Height = 30
    $form.Controls.Add($label)

    $detailsBox = New-Object System.Windows.Forms.TextBox
    $detailsBox.Left = 10
    $detailsBox.Top = 45
    $detailsBox.Width = 640
    $detailsBox.Height = 250
    $detailsBox.Multiline = $true
    $detailsBox.ReadOnly = $true
    $detailsBox.ScrollBars = "Vertical"
    $detailsBox.BackColor = [System.Drawing.SystemColors]::Window
    $detailsBox.TabStop = $false
    $detailsBox.Text = $Details
    $form.Controls.Add($detailsBox)

    $buttonPanel = New-Object System.Windows.Forms.FlowLayoutPanel
    $buttonPanel.Left = 10
    $buttonPanel.Top = 305
    $buttonPanel.Width = 640
    $buttonPanel.Height = 70
    $buttonPanel.FlowDirection = "LeftToRight"
    $buttonPanel.WrapContents = $true
    $form.Controls.Add($buttonPanel)

    $script:CertDialogAction = ""

    $selectCert = New-Object System.Windows.Forms.Button
    $selectCert.Text = "Select cert.pem"
    $selectCert.Width = 140
    $selectCert.Height = 32
    $selectCert.Add_Click({
        $script:CertDialogAction = "selectcert"
        $form.DialogResult = [System.Windows.Forms.DialogResult]::OK
        $form.Close()
    })
    $buttonPanel.Controls.Add($selectCert)

    $selectKey = New-Object System.Windows.Forms.Button
    $selectKey.Text = "Select key.pem"
    $selectKey.Width = 140
    $selectKey.Height = 32
    $selectKey.Add_Click({
        $script:CertDialogAction = "selectkey"
        $form.DialogResult = [System.Windows.Forms.DialogResult]::OK
        $form.Close()
    })
    $buttonPanel.Controls.Add($selectKey)

    if ($ShowOpenFolder) {
        $openFolder = New-Object System.Windows.Forms.Button
        $openFolder.Text = "Open folder"
        $openFolder.Width = 120
        $openFolder.Height = 32
        $openFolder.Add_Click({
            $script:CertDialogAction = "openfolder"
            $form.DialogResult = [System.Windows.Forms.DialogResult]::OK
            $form.Close()
        })
        $buttonPanel.Controls.Add($openFolder)
    }

    $checkNow = New-Object System.Windows.Forms.Button
    $checkNow.Text = "Check now"
    $checkNow.Width = 110
    $checkNow.Height = 32
    $checkNow.Add_Click({
        $script:CertDialogAction = "check"
        $form.DialogResult = [System.Windows.Forms.DialogResult]::OK
        $form.Close()
    })
    $buttonPanel.Controls.Add($checkNow)

    $cancelButton = New-Object System.Windows.Forms.Button
    $cancelButton.Text = "Cancel"
    $cancelButton.Width = 90
    $cancelButton.Height = 32
    $cancelButton.Add_Click({ $form.DialogResult = [System.Windows.Forms.DialogResult]::Cancel })
    $buttonPanel.Controls.Add($cancelButton)

    $form.CancelButton = $cancelButton

    $result = $form.ShowDialog()
    if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
        return $script:CertDialogAction
    }
    return "cancel"
}

function Handle-UserProvidedCerts {
    param([string]$DeploymentPath)

    $sslDir = "$DeploymentPath/ssl"
    $sslCert = "$sslDir/cert.pem"
    $sslKey = "$sslDir/key.pem"

    Invoke-WslExec -Executable "/bin/mkdir" -Arguments @("-p", $sslDir) -Root | Out-Null

    $windowsSslDir = ConvertFrom-WSLPath $sslDir
    if ($windowsSslDir -notmatch '^[A-Za-z]:\\') {
        $windowsSslDir = ""
    }

    $certStatus = "missing"
    $keyStatus = "missing"

    while ($true) {
        $certExists = Test-WslPathExists -Path $sslCert -File -Root
        $keyExists = Test-WslPathExists -Path $sslKey -File -Root
        $certStatus = if ($certExists) { "present" } else { "missing" }
        $keyStatus = if ($keyExists) { "present" } else { "missing" }

        $detailsLines = @(
            "Place the following files in the ssl/ directory:",
            "- cert.pem (certificate, include full chain if provided)",
            "- key.pem (private key)",
            "",
            "WSL path:",
            $sslDir
        )

        if (-not [string]::IsNullOrWhiteSpace($windowsSslDir)) {
            $detailsLines += ""
            $detailsLines += "Windows path:"
            $detailsLines += $windowsSslDir
        }

        $detailsLines += ""
        $detailsLines += ("Status: cert.pem is {0}, key.pem is {1}" -f $certStatus, $keyStatus)
        $detailsText = ($detailsLines -join "`r`n")

        if ($script:UseGuiPrompts) {
            $action = Show-CertDialog -Prompt "Provide your TLS certificates:" -Details $detailsText -ShowOpenFolder:([string]::IsNullOrWhiteSpace($windowsSslDir) -eq $false)
            switch ($action) {
                "selectcert" {
                    $certFile = Show-OpenFileDialog -Title "Select cert.pem" -Filter "PEM files (*.pem)|*.pem|All files (*.*)|*.*"
                    if (-not [string]::IsNullOrWhiteSpace($certFile)) {
                        Copy-ToWSL -WindowsFilePath $certFile -WSLTempPath $sslCert
                    }
                }
                "selectkey" {
                    $keyFile = Show-OpenFileDialog -Title "Select key.pem" -Filter "PEM files (*.pem)|*.pem|All files (*.*)|*.*"
                    if (-not [string]::IsNullOrWhiteSpace($keyFile)) {
                        Copy-ToWSL -WindowsFilePath $keyFile -WSLTempPath $sslKey
                    }
                }
                "openfolder" {
                    if (-not [string]::IsNullOrWhiteSpace($windowsSslDir)) {
                        try {
                            Start-Process -FilePath "explorer.exe" -ArgumentList $windowsSslDir | Out-Null
                        }
                        catch {
                            Write-WarningMsg "Failed to open folder: $($_.Exception.Message)"
                        }
                    }
                }
                "check" {
                    $certExists = Test-WslPathExists -Path $sslCert -File -Root
                    $keyExists = Test-WslPathExists -Path $sslKey -File -Root
                    if ($certExists -and $keyExists) {
                        Write-Success "TLS certificate files found."
                        return $true
                    }
                    [System.Windows.Forms.MessageBox]::Show(
                        "Missing cert.pem or key.pem in the ssl folder. Please place both files and try again.",
                        "Proto Fleet Installer",
                        [System.Windows.Forms.MessageBoxButtons]::OK,
                        [System.Windows.Forms.MessageBoxIcon]::Warning
                    ) | Out-Null
                }
                default {
                    return $false
                }
            }
        }
        else {
            Write-Block $detailsLines
            Write-Host ""

            $certPath = Read-HostLine "Enter path to cert.pem (leave blank if already placed)"
            if (-not [string]::IsNullOrWhiteSpace($certPath)) {
                if (-not (Test-Path -LiteralPath $certPath)) {
                    Write-WarningMsg "File not found: $certPath"
                }
                else {
                    Copy-ToWSL -WindowsFilePath $certPath -WSLTempPath $sslCert
                }
            }

            $keyPath = Read-HostLine "Enter path to key.pem (leave blank if already placed)"
            if (-not [string]::IsNullOrWhiteSpace($keyPath)) {
                if (-not (Test-Path -LiteralPath $keyPath)) {
                    Write-WarningMsg "File not found: $keyPath"
                }
                else {
                    Copy-ToWSL -WindowsFilePath $keyPath -WSLTempPath $sslKey
                }
            }

            $confirm = Read-YesNoPrompt "Check for cert.pem and key.pem now?" -DefaultYes:$true
            if (-not $confirm) {
                return $false
            }

            $certExists = Test-WslPathExists -Path $sslCert -File -Root
            $keyExists = Test-WslPathExists -Path $sslKey -File -Root
            if ($certExists -and $keyExists) {
                Write-Success "TLS certificate files found."
                return $true
            }

            Write-WarningMsg "Missing cert.pem or key.pem in ssl/."
            Write-Host "Please place both files and try again."
        }
    }
}

# ============================================================================
# Exit / Pause / Error
# ============================================================================

function Invoke-Exit {
    param([int]$Code = 0)

    if ($script:IsExe -and -not $env:PROTOFLEET_NO_PAUSE) {
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
        $global:ProtoFleetDidExitPause = $true
    }

    if ($script:TranscriptStarted) {
        try { Stop-Transcript | Out-Null } catch { }
    }
    exit $Code
}

trap {
    Write-ErrorMsg "Unhandled error: $($_.Exception.Message)"
    Write-Host $_.Exception.ToString()
    Invoke-Exit 1
}

# ============================================================================
# System / WSL Helpers
# ============================================================================

function Test-Administrator {
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    return $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Escape-BashSingleQuotes {
    param([string]$Text)
    if ($null -eq $Text) { return "" }
    $replacement = "'" + '"' + "'" + '"' + "'"
    return $Text -replace "'", $replacement
}

function Invoke-Wsl {
    param(
        [string]$Command,
        [switch]$Quiet,
        [switch]$Root
    )

    $distroArgs = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $distroArgs += "-d"
        $distroArgs += $script:WslDistro
    }
    if ($Root) {
        $distroArgs += "-u"
        $distroArgs += "root"
    }

    $normalized = $Command -replace "`r", ""
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($normalized)
    $b64 = [Convert]::ToBase64String($bytes)
    $execCmd = "echo $b64 | base64 -d | bash"
    if ($env:PROTOFLEET_DEBUG -eq "1") {
        Write-Host ("DEBUG: Invoke-Wsl cmd (len {0}): {1}" -f $normalized.Length, ($normalized.Substring(0, [Math]::Min(120, $normalized.Length))))
    }
    if ($Quiet) {
        wsl @distroArgs bash -lc $execCmd 2>$null
    }
    else {
        wsl @distroArgs bash -lc $execCmd
    }
}

function Invoke-WslCapture {
    param([string]$Command)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        return (Invoke-Wsl -Command $Command -Root -Quiet:$false 2>&1)
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Invoke-WslRootCapture {
    param([string]$Command)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $args = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $args += "-d"
            $args += $script:WslDistro
        }
        $normalized = $Command -replace "`r", ""
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($normalized)
        $b64 = [Convert]::ToBase64String($bytes)
        $execCmd = "echo $b64 | base64 -d | bash"
        if ($env:PROTOFLEET_DEBUG -eq "1") {
            Write-Host ("DEBUG: Invoke-WslRootCapture cmd (len {0}): {1}" -f $normalized.Length, ($normalized.Substring(0, [Math]::Min(120, $normalized.Length))))
        }
        return (wsl -u root @args bash -lc $execCmd 2>&1)
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Invoke-WslUserCapture {
    param(
        [string]$User,
        [string]$Command
    )
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $args = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $args += "-d"
            $args += $script:WslDistro
        }
        $normalized = $Command -replace "`r", ""
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($normalized)
        $b64 = [Convert]::ToBase64String($bytes)
        $execCmd = "echo $b64 | base64 -d | bash"
        return (wsl -u $User @args bash -lc $execCmd 2>&1)
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Invoke-WslExec {
    param(
        [string]$Executable,
        [string[]]$Arguments,
        [switch]$Root,
        [switch]$Quiet
    )

    $args = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $args += "-d"
        $args += $script:WslDistro
    }
    if ($Root) {
        $args += "-u"
        $args += "root"
    }
    $args += "--exec"
    $args += $Executable
    if ($Arguments) {
        $args += $Arguments
    }

    if ($Quiet) {
        & wsl.exe @args 2>$null | Out-Null
        return @{ ExitCode = $LASTEXITCODE; Output = "" }
    }

    $out = & wsl.exe @args 2>&1
    return @{ ExitCode = $LASTEXITCODE; Output = $out }
}

function Get-WslDefaultUserName {
    $result = Invoke-WslExec -Executable "/usr/bin/awk" -Arguments @("-F:", '$3==1000{print $1; exit}', "/etc/passwd") -Root
    $out = $result.Output
    if ($out -is [array]) { $out = ($out | Select-Object -First 1) }
    $user = ($out | Out-String).Trim()

    if ([string]::IsNullOrWhiteSpace($user)) {
        $result = Invoke-WslExec -Executable "/usr/bin/awk" -Arguments @("-F:", '$3>=1000 && $1!="nobody"{print $1; exit}', "/etc/passwd") -Root
        $out = $result.Output
        if ($out -is [array]) { $out = ($out | Select-Object -First 1) }
        $user = ($out | Out-String).Trim()
    }

    return $user
}

function Ensure-EnvFileOwnership {
    param([string]$EnvFilePath)

    if ([string]::IsNullOrWhiteSpace($EnvFilePath)) { return }

    $user = Get-WslDefaultUserName
    if ([string]::IsNullOrWhiteSpace($user)) {
        Write-WarningMsg "Could not determine default WSL user; leaving .env ownership unchanged."
        return
    }

    Invoke-WslExec -Executable "/bin/chown" -Arguments @("--", "${user}:${user}", $EnvFilePath) -Root | Out-Null
    Invoke-WslExec -Executable "/bin/chmod" -Arguments @("600", $EnvFilePath) -Root | Out-Null
}

function Get-WslHomeDir {
    param([string]$User)

    if ([string]::IsNullOrWhiteSpace($User)) { return $null }

    $result = Invoke-WslExec -Executable "/usr/bin/getent" -Arguments @("passwd", $User) -Root
    $line = ($result.Output | Out-String).Trim()
    if ($result.ExitCode -eq 0 -and $line -match ":") {
        $parts = $line.Split(":")
        if ($parts.Length -ge 6 -and -not [string]::IsNullOrWhiteSpace($parts[5])) {
            return $parts[5]
        }
    }

    if ($User -eq "root") { return "/root" }
    return "/home/$User"
}

function Resolve-WslPath {
    param([string]$Path)

    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }

    if ($Path -match '^~([^/]*)(/.*)?$') {
        $user = $Matches[1]
        $rest = $Matches[2]
        if ([string]::IsNullOrWhiteSpace($user)) {
            $user = Get-WslDefaultUserName
            if ([string]::IsNullOrWhiteSpace($user)) { $user = "root" }
        }
        $wslHome = Get-WslHomeDir -User $user
        if ([string]::IsNullOrWhiteSpace($wslHome)) { return $Path }
        if ([string]::IsNullOrWhiteSpace($rest)) { return $wslHome }
        return ($wslHome.TrimEnd("/") + $rest)
    }

    return $Path
}

function Ensure-DockerUserAccess {
    Write-Host "Ensuring current WSL user can access Docker..."

    $user = Get-WslDefaultUserName
    if ([string]::IsNullOrWhiteSpace($user)) {
        Write-WarningMsg "Could not determine default WSL user; Docker access may require root."
        return
    }

    Invoke-WslExec -Executable "/usr/sbin/groupadd" -Arguments @("-f", "docker") -Root | Out-Null
    Invoke-WslExec -Executable "/usr/sbin/usermod" -Arguments @("-aG", "docker", $user) -Root | Out-Null

    if (Test-WslPathExists -Path "/var/run/docker.sock" -File -Root) {
        Invoke-WslExec -Executable "/bin/chgrp" -Arguments @("docker", "/var/run/docker.sock") -Root | Out-Null
        Invoke-WslExec -Executable "/bin/chmod" -Arguments @("660", "/var/run/docker.sock") -Root | Out-Null
    }

    Invoke-WslRootCapture "systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || true" | Out-Null

    $check = Invoke-WslExec -Executable "/usr/bin/id" -Arguments @("-nG", $user) -Root
    if ($check.ExitCode -eq 0 -and ($check.Output | Out-String) -match "\bdocker\b") {
        Write-Success "User '$user' is in the docker group."
    }
    else {
        Write-WarningMsg "User '$user' may not have Docker group access yet."
        Write-Host "Try running 'wsl.exe --shutdown' and re-running this installer."
    }
}

function Ensure-WslAutoStartTask {
    if ($Silent) {
        return
    }

    $taskName = "ProtoFleet-StartWSL"
    $distro = $script:WslDistro
    $arguments = if ([string]::IsNullOrWhiteSpace($distro)) {
        '-u root --exec /bin/sh -lc "systemctl start docker || service docker start"'
    }
    else {
        ('-d ' + $distro + ' -u root --exec /bin/sh -lc "systemctl start docker || service docker start"')
    }

    $existingTask = $null
    try {
        $existingTask = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    }
    catch {
        $existingTask = $null
    }

    if ($existingTask) {
        $existingAction = $existingTask.Actions | Select-Object -First 1
        $actionMatch = $false
        if ($existingAction) {
            $actionMatch = ($existingAction.Execute -ieq "wsl.exe" -and $existingAction.Arguments -eq $arguments)
        }

        $taskEnabled = $true
        if ($existingTask.State -eq "Disabled") {
            $taskEnabled = $false
        }
        elseif ($existingTask.Settings -and $existingTask.Settings.Enabled -eq $false) {
            $taskEnabled = $false
        }

        if ($actionMatch -and $taskEnabled) {
            Write-Success "Auto-start task already configured: $taskName"
            return
        }

        Write-Host "Auto-start task exists but needs updates."
        $update = Read-YesNoPrompt "Update/enable the auto-start task now?" -DefaultYes:$true
        if (-not $update) {
            Write-Host "Auto-start task left unchanged."
            return
        }
    }
    else {
        Write-Host "Set up WSL + Docker to start at Windows login?"
        $consent = Read-YesNoPrompt "Enable auto-start on login? (No = you'll start WSL manually after reboot)" -DefaultYes:$true
        if (-not $consent) {
            Write-Host "Auto-start not enabled."
            return
        }
    }

    Write-Host "Configuring WSL auto-start task..."

    $manualCmd = ('schtasks /Create /F /TN "{0}" /SC ONLOGON /DELAY 0000:10 /RL LIMITED /RU "{1}" /TR "wsl.exe {2}"' -f $taskName, $env:USERNAME, $arguments)

    try {
        $action = New-ScheduledTaskAction -Execute "wsl.exe" -Argument $arguments
        $trigger = New-ScheduledTaskTrigger -AtLogOn -Delay "00:00:10"
        $principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited
        $task = New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal

        Register-ScheduledTask -TaskName $taskName -InputObject $task -Force | Out-Null
        Write-Success "Created scheduled task: $taskName"
        Write-Host "This will start WSL and Docker at login (10s delay)."

        $verifyOk = $false
        try {
            $verifyTask = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
            if ($verifyTask) { $verifyOk = $true }
        }
        catch {
            $verifyOk = $false
        }

        if (-not $verifyOk) {
            Write-WarningMsg "Scheduled task was not found after creation. Auto-start may not work."
            Write-Host "You can create it manually with:"
            Write-Host $manualCmd
            if ($script:UseGuiPrompts) {
                [System.Windows.Forms.MessageBox]::Show(
                    "The auto-start task was not found after creation. Auto-start may not work.`n`nYou can create it manually using:`n$manualCmd",
                    "Proto Fleet Installer",
                    [System.Windows.Forms.MessageBoxButtons]::OK,
                    [System.Windows.Forms.MessageBoxIcon]::Warning
                ) | Out-Null
            }
        }
    }
    catch {
        Write-WarningMsg "Failed to create scheduled task for WSL auto-start."
        Write-Host $_.Exception.Message
        Write-Host "You can create it manually with:"
        Write-Host $manualCmd
        if ($script:UseGuiPrompts) {
            [System.Windows.Forms.MessageBox]::Show(
                "Failed to create the auto-start task. Auto-start may not work.`n`nYou can create it manually using:`n$manualCmd",
                "Proto Fleet Installer",
                [System.Windows.Forms.MessageBoxButtons]::OK,
                [System.Windows.Forms.MessageBoxIcon]::Warning
            ) | Out-Null
        }
    }
}

function Test-WslPathExists {
    param(
        [string]$Path,
        [switch]$File,
        [switch]$Directory,
        [switch]$Root
    )

    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }
    $testFlag = "-e"
    if ($File) { $testFlag = "-f" }
    if ($Directory) { $testFlag = "-d" }

    $result = Invoke-WslExec -Executable "/usr/bin/test" -Arguments @($testFlag, $Path) -Root:$Root -Quiet
    return ($result.ExitCode -eq 0)
}

function Invoke-ProcessCore {
    param(
        [string]$FileName,
        [string]$Arguments,
        [string]$Activity = "Working",
        [int]$TimeoutSeconds = 900,
        [switch]$UseSpinner
    )

    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $FileName
    $startInfo.Arguments = $Arguments
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true

    $proc = New-Object System.Diagnostics.Process
    $proc.StartInfo = $startInfo
    [void]$proc.Start()

    $stdoutTask = $proc.StandardOutput.ReadToEndAsync()
    $stderrTask = $proc.StandardError.ReadToEndAsync()

    if ($UseSpinner -and -not $env:PROTOFLEET_NO_SPINNER) {
        $spinner = $script:SpinnerFrames
        $idx = 0
        $elapsed = 0
        while (-not $proc.HasExited) {
            $spin = $spinner[$idx % $spinner.Count]
            [Console]::Write("`r$Activity $spin  Elapsed ${elapsed}s")
            Start-Sleep -Milliseconds 200
            $idx++
            if ($idx % 5 -eq 0) { $elapsed++ }
            if ($TimeoutSeconds -gt 0 -and $elapsed -ge $TimeoutSeconds) {
                try { $proc.Kill() | Out-Null } catch {}
                $global:LASTEXITCODE = 124
                [Console]::Write("`r")
                Write-Host ""
                return @{ ExitCode = 124; Output = "Timed out after $TimeoutSeconds seconds." }
            }
        }
        [Console]::Write("`r")
        Write-Host ""
    }
    elseif (-not $UseSpinner -and $TimeoutSeconds -gt 0) {
        if (-not $proc.WaitForExit($TimeoutSeconds * 1000)) {
            try { $proc.Kill() | Out-Null } catch {}
            $global:LASTEXITCODE = 124
            return @{ ExitCode = 124; Output = "Timed out after $TimeoutSeconds seconds." }
        }
    }

    $proc.WaitForExit()
    $global:LASTEXITCODE = $proc.ExitCode
    $outText = $stdoutTask.Result
    $errText = $stderrTask.Result
    $combined = ($outText + $errText)
    return @{ ExitCode = $proc.ExitCode; Output = $combined }
}

function Invoke-ProcessWithSpinner {
    param(
        [string]$FileName,
        [string]$Arguments,
        [string]$Activity = "Working",
        [int]$TimeoutSeconds = 900
    )

    $result = Invoke-ProcessCore -FileName $FileName -Arguments $Arguments -Activity $Activity -TimeoutSeconds $TimeoutSeconds -UseSpinner
    $global:LASTEXITCODE = $result.ExitCode
    return $result
}

function Invoke-ProcessCapture {
    param(
        [string]$FileName,
        [string]$Arguments,
        [int]$TimeoutSeconds = 60
    )
    $result = Invoke-ProcessCore -FileName $FileName -Arguments $Arguments -Activity "" -TimeoutSeconds $TimeoutSeconds
    $global:LASTEXITCODE = $result.ExitCode
    return $result
}

function Clear-WslStatusCache {
    $script:CachedWslStatusText = $null
    $script:CachedWslStatusExitCode = $null
}

function Invoke-WslShutdownAndRefresh {
    param(
        [int]$WaitSeconds = 3
    )
    try {
        & wsl.exe --shutdown | Out-Null
    }
    catch {
        # Ignore shutdown failures
    }
    if ($WaitSeconds -gt 0) {
        Start-Sleep -Seconds $WaitSeconds
    }
    Clear-WslStatusCache
}

function Ensure-WslAutomountEnabled {
    Write-Host "Ensuring WSL automount is enabled..."
    Invoke-WslRootCapture @'
set -e
if [ ! -f /etc/wsl.conf ]; then
  printf "[automount]\nenabled=true\n" > /etc/wsl.conf
  exit 0
fi
if grep -q "^\[automount\]" /etc/wsl.conf; then
  if grep -q "enabled" /etc/wsl.conf; then
    sed -i "s/enabled *= *.*/enabled=true/" /etc/wsl.conf
  else
    sed -i "/^\[automount\]/a enabled=true" /etc/wsl.conf
  fi
else
  printf "\n[automount]\nenabled=true\n" >> /etc/wsl.conf
fi
'@
    Invoke-WslShutdownAndRefresh -WaitSeconds 3
}

function Ensure-WslAutomountReady {
    Write-Host "Checking WSL automount status..."
    $mountOk = Test-WslMountAvailable
    if ($mountOk) {
        Write-Success "WSL automount is available (/mnt/c)."
        return $true
    }

    Write-WarningMsg "WSL automount is not available. Attempting to enable it."
    Ensure-WslAutomountEnabled

    $mountOk = Test-WslMountAvailable
    if ($mountOk) {
        Write-Success "WSL automount enabled (/mnt/c available)."
        return $true
    }

    Write-WarningMsg "WSL automount is still unavailable."
    Write-Host "We'll fall back to copying the deployment into WSL."
    Write-Host "If you want to fix automount manually, ensure /etc/wsl.conf contains:"
    Write-Host "[automount]"
    Write-Host "enabled=true"
    Write-Host "Then run: wsl.exe --shutdown"
    return $false
}

function Test-WslMountAvailable {
    $args = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $args += "-d"
        $args += $script:WslDistro
    }
    $args += "-u"
    $args += "root"
    $args += "--"
    $args += "test"
    $args += "-d"
    $args += "/mnt"
    & wsl.exe @args | Out-Null
    if ($LASTEXITCODE -ne 0) { return $false }

    $args2 = @()
    if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $args2 += "-d"
        $args2 += $script:WslDistro
    }
    $args2 += "-u"
    $args2 += "root"
    $args2 += "--"
    $args2 += "test"
    $args2 += "-d"
    $args2 += "/mnt/c"
    & wsl.exe @args2 | Out-Null
    return ($LASTEXITCODE -eq 0)
}

function Write-WslUpdateBlockedGuidance {
    Write-WarningMsg "WSL still reports that an update is required after running wsl.exe --update."
    Write-Host ""
    Write-Host "This usually means the WSL kernel update did not apply."
    Write-Host "Common causes:"
    Write-Host "1. Microsoft Store updates are blocked by policy"
    Write-Host "2. Windows is missing required updates for WSL2"
    Write-Host ""
    Write-Host "Next steps:"
    Write-Host "1. If Microsoft Store is available, open it and update 'Windows Subsystem for Linux'."
    Write-Host "2. If Store is blocked, install the WSL kernel update manually from Microsoft:"
    Write-Host "   https://aka.ms/wsl2kernel"
    Write-Host "3. Reboot Windows and run this installer again."
}

function Ensure-DistroInitialized {
    param([string]$DistroName)

    if ([string]::IsNullOrWhiteSpace($DistroName)) { return }

    $checkUser = wsl -d $DistroName -u root bash -lc "getent passwd 1000 | cut -d: -f1" 2>$null
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($checkUser)) {
        return
    }

    Write-Host ""
    Write-Host "Launching $DistroName for first-time setup..."
    Write-Host "Please create a username and password when prompted."
    Write-Host "After the setup completes, type: exit"
    Write-Host ""

    $launchAttempts = 0
    $maxLaunchAttempts = 2
    $setupComplete = $false

    while (-not $setupComplete -and $launchAttempts -lt $maxLaunchAttempts) {
        $launchAttempts++

        $launchProc = Start-Process "wsl.exe" -ArgumentList @("-d", $DistroName) -PassThru
        while (-not $launchProc.HasExited) {
            [Console]::Write("`r$DistroName setup in progress (complete prompts in the $DistroName window)...")
            Start-Sleep -Milliseconds 500
        }
        [Console]::Write("`r")
        Write-Host ""

        $checkUser = wsl -d $DistroName -u root bash -lc "getent passwd 1000 | cut -d: -f1" 2>$null
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($checkUser)) {
            $setupComplete = $true
            break
        }

        if ($launchAttempts -lt $maxLaunchAttempts) {
            $prompt = "$DistroName setup is required to continue.`n`nPlease complete the prompts (create a user and password), then type 'exit'.`n`nRe-launch $DistroName now?"
            if ($Silent) {
                Write-WarningMsg "$DistroName setup not completed. Please open it, complete setup, then re-run this installer."
                Invoke-Exit 1
            }
            $relaunch = Read-YesNoPrompt $prompt -DefaultYes:$true
            if (-not $relaunch) {
                Write-WarningMsg "$DistroName setup not completed. Please complete it and re-run this installer."
                Invoke-Exit 1
            }
        }
    }

    if (-not $setupComplete) {
        Write-WarningMsg "$DistroName setup not completed. Please open it, complete setup, then re-run this installer."
        Invoke-Exit 1
    }
}

function Confirm-Wsl2Upgrade {
    param([string]$Prompt)

    if ($Silent) {
        Write-ErrorMsg "WSL2 is required but Silent mode is enabled."
        Invoke-Exit 1
    }

    $confirm = Read-YesNoPrompt $Prompt -DefaultYes:$true
    if (-not $confirm) {
        Write-WarningMsg "Cannot continue without WSL2. Docker requires WSL2 to run."
        Invoke-Exit 1
    }

    return $true
}

function Invoke-WslUpgradeWithRetry {
    param(
        [string]$CommandArgs,
        [string]$Activity,
        [int]$TimeoutSeconds = 60,
        [string]$SuccessMessage
    )

    Write-Step $Activity
    $attempts = 0
    while ($attempts -lt 2) {
        $attempts++
        $result = Invoke-ProcessWithSpinner -FileName "wsl.exe" -Arguments $CommandArgs -Activity $Activity -TimeoutSeconds $TimeoutSeconds
        if ($result.ExitCode -eq 0) {
            if (-not [string]::IsNullOrWhiteSpace($SuccessMessage)) {
                Write-Success $SuccessMessage
            }
            return $true
        }

        if (Test-WslInstallNeededText -Text $result.Output) {
            Ensure-WslPackageInstalled
            continue
        }

        $needsUpdate = (Get-WslUpdateState -Text $result.Output) -eq "required"
        if (-not $needsUpdate) {
            Clear-WslStatusCache
            $needsUpdate = Test-WslUpdateNeeded
        }

        if ($needsUpdate) {
            Ensure-WslUpdated | Out-Null
            if (Test-WslUpdateNeeded) {
                Write-WslUpdateBlockedGuidance
                Invoke-Exit 1
            }
            continue
        }

        Write-ErrorMsg "Failed to run '$CommandArgs': $($result.Output)"
        Invoke-Exit 1
    }

    Write-ErrorMsg "Failed to run '$CommandArgs' after updating WSL."
    Invoke-Exit 1
}

function Invoke-WslCommandWithSpinner {
    param(
        [string]$Command,
        [string]$Activity = "Working",
        [int]$TimeoutSeconds = 900,
        [switch]$Root
    )

    $normalizedCommand = $Command -replace "`r", ""
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($normalizedCommand)
    $b64 = [Convert]::ToBase64String($bytes)
    $escaped = "echo $b64 | base64 -d | bash"
    $argsText = ""
    $userPrefix = if ($Root) { "-u root " } else { "" }

    if ([string]::IsNullOrWhiteSpace($script:WslDistro)) {
        $argsText = "${userPrefix}bash -lc ""$escaped"""
    }
    else {
        $argsText = "-d $script:WslDistro ${userPrefix}bash -lc ""$escaped"""
    }

    $result = Invoke-ProcessWithSpinner -FileName "wsl.exe" -Arguments $argsText -Activity $Activity -TimeoutSeconds $TimeoutSeconds
    if ($result.ExitCode -ne 0) {
        $script:LastCommandOutput = $result.Output
    }
    return $result
}

function Invoke-WithRetryStream {
    param(
        [scriptblock]$Operation,
        [int]$MaxAttempts = 5,
        [int]$DelaySeconds = 5,
        [string]$ActionDescription = "Operation",
        [scriptblock]$OnFailure = $null
    )

    for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
        try {
            $result = & $Operation
            if ($result -is [int]) {
                $global:LASTEXITCODE = $result
                if ($result -eq 0) { return $true }
            }
            elseif ($result -is [bool]) {
                if ($result) { return $true }
            }
            elseif ($LASTEXITCODE -eq 0) {
                return $true
            }
        }
        catch {
            # fall through
        }

        if ($attempt -lt $MaxAttempts) {
            Write-WarningMsg "$ActionDescription failed. Retrying in $DelaySeconds seconds..."
            if ($null -ne $OnFailure) {
                try {
                    $override = & $OnFailure
                }
                catch {
                    Write-WarningMsg "Diagnostic step failed: $($_.Exception.Message)"
                    $override = $false
                }
                if ($override -eq $true) {
                    return $true
                }
            }
            Start-Sleep -Seconds $DelaySeconds
            $DelaySeconds = [Math]::Min($DelaySeconds * 2, 60)
        }
    }

    return $false
}

function Test-WSLInstalled {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        if ($null -ne $script:CachedWslStatusExitCode) {
            return $script:CachedWslStatusExitCode -eq 0
        }
        $result = Invoke-ProcessCapture -FileName "wsl.exe" -Arguments "--status" -TimeoutSeconds 60
        $script:CachedWslStatusText = $result.Output
        $script:CachedWslStatusExitCode = $result.ExitCode
        return $result.ExitCode -eq 0
    }
    catch {
        return $false
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Get-WslStatusText {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        if ($null -ne $script:CachedWslStatusText -and $script:CachedWslStatusExitCode -eq 0) {
            return $script:CachedWslStatusText
        }
        $result = Invoke-ProcessCapture -FileName "wsl.exe" -Arguments "--status" -TimeoutSeconds 60
        if ($null -eq $result) { return "" }
        return $result.Output
    }
    catch {
        return ""
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Get-WslUpdateState {
    param([string]$Text)

    $value = $Text
    if ([string]::IsNullOrWhiteSpace($value)) {
        $value = Get-WslStatusText
    }
    if ([string]::IsNullOrWhiteSpace($value)) { return "none" }

    $flat = ($value -replace "\s+", "")
    if ($value -match "must be updated to the latest version" -or
        $value -match "wsl\.exe --update" -or
        $flat -match "WindowsSubsystemforLinuxmustbeupdatedtothelatestversiontoproceed" -or
        $flat -match "wsl\.exe--update") {
        return "required"
    }

    return "none"
}

function Test-WslUpdateNeeded {
    return (Get-WslUpdateState) -eq "required"
}

function Test-WslInstallNeededText {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return $false }
    $flat = ($Text -replace "\s+", "")
    $letters = ($Text.ToLower() -replace "[^a-z0-9]", "")
    return ($Text -match "Press any key to install Windows Subsystem for Linux" -or
            $flat -match "PressanykeytoinstallWindowsSubsystemforLinux" -or
            $flat -match "aka\.ms/wslinstall" -or
            $letters -match "pressanykeytoinstallwindowssubsystemforlinux" -or
            $letters -match "wslinstall")
}

function Ensure-WslPackageInstalled {
    if ($Silent) {
        Write-ErrorMsg "WSL is not installed and Silent mode is enabled. Run: wsl.exe --install"
        Invoke-Exit 1
    }

    $prompt = @"
Windows Subsystem for Linux (WSL) is not installed.

We need WSL to run Docker for Proto Fleet.

Install WSL now? This will run: wsl.exe --install --no-launch
"@

    $confirm = Read-YesNoPrompt $prompt -DefaultYes:$true
    if (-not $confirm) {
        Write-WarningMsg "Cannot continue without installing WSL."
        Invoke-Exit 1
    }

    Write-Step "Installing WSL..."
    $result = Invoke-ProcessWithSpinner -FileName "wsl.exe" -Arguments "--install --no-launch" -Activity "Installing WSL" -TimeoutSeconds 900
    if ($result.ExitCode -ne 0) {
        Write-ErrorMsg "WSL install failed: $($result.Output)"
        Invoke-Exit 1
    }
    Invoke-WslShutdownAndRefresh -WaitSeconds 3
    Write-Success "WSL installed successfully"
}

function Ensure-WslUpdated {
    if (-not (Test-WslUpdateNeeded)) { return $true }

    if ($Silent) {
        Write-ErrorMsg "WSL update required but Silent mode is enabled. Run: wsl.exe --update"
        Invoke-Exit 1
    }

    $prompt = @"
Windows Subsystem for Linux (WSL) needs an update to continue.

Why: Proto Fleet runs Docker inside WSL, which depends on the WSL kernel.
Pros: fixes kernel bugs, improves stability and performance.
Cons: downloads and installs an update; may take a few minutes.

Update WSL now? This will run: wsl.exe --update
"@

    $confirm = Read-YesNoPrompt $prompt -DefaultYes:$true
    if (-not $confirm) {
        Write-WarningMsg "Cannot continue without updating WSL (Docker requires the updated WSL kernel)."
        Invoke-Exit 1
    }

    Write-Step "Updating WSL..."
    try {
        & wsl.exe --update | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "wsl.exe --update failed with exit code $LASTEXITCODE"
        }
    }
    catch {
        Write-ErrorMsg "WSL update failed: $($_.Exception.Message)"
        Invoke-Exit 1
    }

    Invoke-WslShutdownAndRefresh -WaitSeconds 3
    Write-Success "WSL updated successfully"
    return $true
}

function Ensure-WSL2Default {
    Clear-WslStatusCache
    # Sets the global default for NEW distros (does not upgrade existing distros).
    $status = Get-WslStatusText
    $statusNorm = ($status -replace "[^a-zA-Z0-9]", "").ToLower()
    if ($status -match "Default Version:\s*2" -or $statusNorm -match "defaultversion2") {
        return $true
    }

    if ($Silent) {
        Write-ErrorMsg "WSL2 is required but Silent mode is enabled. Run: wsl.exe --set-default-version 2"
        Invoke-Exit 1
    }

    $prompt = @"
WSL is currently set to version 1 by default.

Why we need WSL2: Proto Fleet runs Docker inside WSL, and Docker requires WSL2's full Linux kernel.
Pros: full Linux kernel, better Docker compatibility, faster filesystem performance for containers.
Cons: uses virtualization, may consume slightly more disk/memory, and conversion can take a few minutes.

Switch the default WSL version to 2 now? This will run: wsl.exe --set-default-version 2
"@

    if (-not (Confirm-Wsl2Upgrade -Prompt $prompt)) { return $false }

    Invoke-WslUpgradeWithRetry -CommandArgs "--set-default-version 2" -Activity "Configuring WSL2 as default version" -TimeoutSeconds 60 -SuccessMessage "WSL2 set as default version"
}

function Ensure-WslDistroIsWsl2 {
    param([string]$DistroName)

    # Upgrades a SPECIFIC existing distro to WSL2; defaults do not affect already-installed distros.
    if ([string]::IsNullOrWhiteSpace($DistroName)) { return $true }

    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $line = wsl --list --verbose 2>&1 | Select-String $DistroName | Select-Object -First 1
        if ($null -eq $line) { return $true }

        $version = ($line -replace "^\*\s+", "" ) -split "\s+"
        $verValue = $version[-1]
        if ($verValue -ne "1") { return $true }
    }
    finally {
        $ErrorActionPreference = $prev
    }

    if ($Silent) {
        Write-ErrorMsg "WSL2 is required but Silent mode is enabled. Run: wsl.exe --set-version $DistroName 2"
        Invoke-Exit 1
    }

    $prompt = @"
Your WSL distribution '$DistroName' is running on WSL1.

Why we need WSL2: Proto Fleet runs Docker inside WSL, and Docker requires WSL2's full Linux kernel.
Pros: full Linux kernel, better Docker compatibility, faster filesystem performance for containers.
Cons: uses virtualization, may consume slightly more disk/memory, and conversion can take a few minutes.

Upgrade '$DistroName' to WSL2 now? This will run: wsl.exe --set-version $DistroName 2
"@

    if (-not (Confirm-Wsl2Upgrade -Prompt $prompt)) { return $false }

    Invoke-WslUpgradeWithRetry -CommandArgs "--set-version $DistroName 2" -Activity "Upgrading $DistroName to WSL2" -TimeoutSeconds 600 -SuccessMessage "$DistroName upgraded to WSL2"
}

function Select-WslDistro {
    $script:WslDistro = ""
    $rawList = wsl --list --verbose 2>&1
    if ($LASTEXITCODE -ne 0) { return }

    $distros = $rawList | Where-Object {
        $_ -and $_ -notmatch '^Windows Subsystem for Linux Distributions:' -and $_ -notmatch '^\s*NAME\s+STATE\s+VERSION'
    }

    $default = $distros | Where-Object { $_ -match '^\*' } | ForEach-Object { ($_ -replace '^\*\s+', '').Split()[0] } | Select-Object -First 1
    if ($default) {
        $script:WslDistro = $default
        Write-Success "Using WSL distribution: $script:WslDistro"
        return
    }

    if ($distros -contains "Ubuntu") {
        $script:WslDistro = "Ubuntu"
        Write-Success "Using WSL distribution: Ubuntu"
        return
    }

    $first = $distros | Select-Object -First 1
    if ($first) {
        $script:WslDistro = ($first -replace '^\*\s+', '').Split()[0]
        Write-Success "Using WSL distribution: $script:WslDistro"
    }
}

function Test-DockerInWSL {
    try {
        Invoke-WslRootCapture "docker info" | Out-Null
        return ($LASTEXITCODE -eq 0)
    }
    catch {
        return $false
    }
}

# ============================================================================
# WSL / Docker Setup
# ============================================================================

function Test-SystemRequirements {
    Write-Step "Checking system requirements..."

    $warnings = @()

    $osInfo = Get-CimInstance Win32_OperatingSystem
    $buildNumber = [int]$osInfo.BuildNumber

    if ($osInfo.Caption -match "Windows 11") {
        Write-Success "Windows 11 detected (Build $buildNumber)"
    }
    else {
        if ($buildNumber -lt $MIN_WIN10_BUILD) {
            Write-ErrorMsg "Windows build $buildNumber is too old for WSL2 (minimum $MIN_WIN10_BUILD)"
            Invoke-Exit 1
        }
        Write-Success "Windows 10 detected (Build $buildNumber)"
    }

    $ramGB = [math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB)
    if ($ramGB -lt $MIN_RAM_GB) {
        $warnings += "System RAM is ${ramGB}GB (recommended: ${MIN_RAM_GB}GB+)"
    }
    else {
        Write-Success "System RAM: ${ramGB}GB"
    }

    $drive = (Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'")
    $diskGB = [math]::Round($drive.FreeSpace / 1GB)
    if ($diskGB -lt $MIN_DISK_GB) {
        $warnings += "C: free space is ${diskGB}GB (recommended: ${MIN_DISK_GB}GB+)"
    }
    else {
        Write-Success "C: free space: ${diskGB}GB"
    }

    if ($warnings.Count -gt 0) {
        Write-WarningMsg "System requirements warnings:"
        foreach ($w in $warnings) { Write-Host "  - $w" }
    }
}

function Request-Reboot {
    param([string]$Reason)

    $msg = "A reboot is required to continue.`n`nReason: $Reason`n`nReboot now?"
    if ($Silent) {
        Write-WarningMsg "Reboot required but Silent mode is enabled. Please reboot and run again."
        Invoke-Exit 2
    }

    $confirm = Read-YesNoPrompt $msg -DefaultYes:$true
    if (-not $confirm) {
        Write-WarningMsg "Reboot required. Please reboot and run again."
        Invoke-Exit 2
    }

    try {
        Restart-Computer -Force
    }
    catch {
        try {
            Start-Process -FilePath "shutdown.exe" -ArgumentList "/r /t 0" -WindowStyle Hidden | Out-Null
        }
        catch {
            Write-WarningMsg "Failed to initiate reboot automatically. Please reboot manually."
        }
    }
    Invoke-Exit 0
}

function Enable-WSLFeature {
    Write-Step "Enabling WSL features..."

    $needsReboot = $false

    $features = @(
        "Microsoft-Windows-Subsystem-Linux",
        "VirtualMachinePlatform"
    )

    foreach ($feature in $features) {
        $state = (Get-WindowsOptionalFeature -Online -FeatureName $feature).State
        if ($state -ne "Enabled") {
            Write-Host "Enabling feature: $feature"
            try {
                $result = Enable-WindowsOptionalFeature -Online -FeatureName $feature -NoRestart -All
                if ($result.RestartNeeded -eq $true) {
                    $needsReboot = $true
                }
            }
            catch {
                Write-ErrorMsg "Failed to enable feature: $feature"
                Write-Host $_.Exception.ToString()
                Invoke-Exit 1
            }
        }
        else {
            Write-Host "Feature already enabled: $feature"
        }
    }

    if ($needsReboot) {
        Request-Reboot -Reason "WSL features enabled"
    }

    Write-Success "WSL features are enabled"
}

function Set-WSL2AsDefault {
    Ensure-WslUpdated | Out-Null
    Ensure-WSL2Default | Out-Null
}

function Install-WSLDistribution {
    Write-Step "Checking for WSL distribution..."

    Ensure-WslUpdated | Out-Null

    $rawList = wsl --list --verbose 2>&1
    $distros = $rawList | Where-Object {
        $_ -and $_ -notmatch '^Windows Subsystem for Linux Distributions:' -and $_ -notmatch '^\s*NAME\s+STATE\s+VERSION'
    }

    if ($LASTEXITCODE -ne 0 -or $distros.Count -eq 0) {
        Write-Host "No WSL distribution found. Installing Ubuntu..."

        try {
            $installResult = Invoke-ProcessWithSpinner -FileName "wsl.exe" -Arguments "--install -d Ubuntu --no-launch" -Activity "Downloading and installing Ubuntu" -TimeoutSeconds 1800
            if ($installResult.ExitCode -ne 0) {
                throw "wsl.exe --install failed with exit code $($installResult.ExitCode)"
            }
            Write-Success "Ubuntu installed successfully"

            Ensure-DistroInitialized -DistroName "Ubuntu"
        }
        catch {
            Write-ErrorMsg "Failed to install Ubuntu: $_"
            Write-Host ""
            Write-Host "Please install Ubuntu manually:"
            Write-Host "1. Open Microsoft Store"
            Write-Host "2. Search for 'Ubuntu'"
            Write-Host "3. Install Ubuntu 22.04 LTS"
            Write-Host "4. Run this installer again"
            Invoke-Exit 1
        }
    }
    else {
        Write-Success "WSL distribution already exists"

        $defaultDistro = wsl --list --verbose 2>&1 | Select-String "^\*" | ForEach-Object { $_ -replace "^\*\s+", "" } | ForEach-Object { ($_ -split "\s+")[0] }

        if ($defaultDistro) {
            Ensure-WslDistroIsWsl2 -DistroName $defaultDistro | Out-Null
        }

        $targetDistro = if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) { $script:WslDistro } else { "Ubuntu" }
        Ensure-DistroInitialized -DistroName $targetDistro
    }
}

function Ensure-SystemdEnabled {
    Write-Host "Ensuring systemd is enabled in WSL..."
    $scriptText = @"
set -e
changed=0
if [ ! -f /etc/wsl.conf ]; then
 printf "[boot]\nsystemd=true\n" > /etc/wsl.conf
 changed=1
else
 if ! grep -q "^\[boot\]" /etc/wsl.conf; then
   printf "\n[boot]\nsystemd=true\n" >> /etc/wsl.conf
   changed=1
 elif grep -q "^[[:space:]]*systemd[[:space:]]*=" /etc/wsl.conf; then
   sed -i "s/^[[:space:]]*systemd[[:space:]]*=.*/systemd=true/" /etc/wsl.conf
   changed=1
 else
   sed -i "/^\[boot\]/a systemd=true" /etc/wsl.conf
   changed=1
 fi
fi
if [ "$changed" -eq 1 ]; then
 echo "changed"
fi
"@
    $result = Invoke-WslRootCapture $scriptText
    if ($result -match "changed") {
        Write-Host "Systemd setting updated. Restarting WSL to apply..."
        try { & wsl.exe --shutdown | Out-Null } catch {}
        Start-Sleep -Seconds 2
    }
    Write-Host "Systemd configuration checked."
}
function Install-DockerInWSL {
    Write-Step "Checking Docker installation in WSL..."

    $dockerInstalled = Invoke-WslRootCapture "command -v docker"

    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($dockerInstalled)) {
        Write-Host "Installing Docker Engine in WSL..."
        Write-Host "This may take several minutes..."

        $installOk = $false
        $lastOutput = ""
        $lastExitCode = $null
        $delaySeconds = 5
        for ($attempt = 1; $attempt -le 5; $attempt++) {
            try {
                $installCmd = @(
                    'set -e',
                    'printf "" > /tmp/pf-docker-install.log',
                    'export DEBIAN_FRONTEND=noninteractive CI=1 TERM=dumb LC_ALL=C',
                    'rm -rf /var/lib/apt/lists/* >/dev/null 2>&1 || true',
                    'apt-get clean >/dev/null 2>&1 || true',
                    'curl -fsSL https://get.docker.com | sh -s -- >>/tmp/pf-docker-install.log 2>&1'
                ) -join "; "
                $result = Invoke-WslCommandWithSpinner -Command $installCmd -Activity "Installing Docker Engine" -TimeoutSeconds 900 -Root
                $lastExitCode = $result.ExitCode
                $lastOutput = $result.Output
                if ($result.ExitCode -eq 0) {
                    $installOk = $true
                    break
                }
            }
            catch {
                $lastOutput = $_.Exception.ToString()
            }

            if ($attempt -lt 5) {
                $logTail = Invoke-WslRootCapture "tail -n 120 /tmp/pf-docker-install.log 2>/dev/null || true"
                if (-not [string]::IsNullOrWhiteSpace($logTail)) {
                    Write-Host "Last output from installer:"
                    Write-Host $logTail
                }

                $isHashMismatch = $false
                if ($logTail -match "Hash Sum mismatch" -or $lastOutput -match "Hash Sum mismatch") {
                    $isHashMismatch = $true
                }

                if ($isHashMismatch) {
                    Write-WarningMsg "Detected apt Hash Sum mismatch. Cleaning apt cache and waiting before retry..."
                    Invoke-WslRootCapture @"
set -e
rm -rf /var/lib/apt/lists/* || true
apt-get clean >/dev/null 2>&1 || true
"@
                    Start-Sleep -Seconds 20
                }

                Write-WarningMsg "Docker installation failed. Retrying in $delaySeconds seconds..."
                if (-not [string]::IsNullOrWhiteSpace($lastOutput)) { Write-Host $lastOutput }
                if ($null -ne $lastExitCode) { Write-Host "Exit code: $lastExitCode" }
                Start-Sleep -Seconds $delaySeconds
                $delaySeconds = [Math]::Min($delaySeconds * 2, 60)
            }
        }

        if (-not $installOk) {
            $logTail = Invoke-WslRootCapture "tail -n 200 /tmp/pf-docker-install.log 2>/dev/null || true"
            if (-not [string]::IsNullOrWhiteSpace($logTail)) {
                Write-Host "Last output from installer:"
                Write-Host $logTail
            }
            if (-not [string]::IsNullOrWhiteSpace($lastOutput)) { Write-Host $lastOutput }
            if ($null -ne $lastExitCode) { Write-Host "Exit code: $lastExitCode" }
            $dockerBin = Invoke-WslRootCapture "command -v docker"
            if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($dockerBin)) {
                Write-WarningMsg "Docker CLI is present despite installer errors. Continuing."
                $installOk = $true
            }
            else {
                Write-Host "You can inspect the full log inside WSL:"
                Write-Host "wsl -u root bash -lc `"cat /tmp/pf-docker-install.log`""
                Write-ErrorMsg "Docker installation failed"
                Invoke-Exit 1
            }
        }

        Write-Success "Docker Engine installed"
    }
    else {
        Write-Success "Docker Engine already installed"
    }

    Write-Host "Enabling Docker to start on boot..."
    Invoke-WslRootCapture "systemctl enable docker 2>/dev/null || true"

    Write-Host "Starting Docker daemon..."
    Invoke-WslRootCapture "service docker start 2>/dev/null || systemctl start docker 2>/dev/null"

    Write-Host "Waiting for Docker daemon to be ready..."
    $retries = 20
    for ($i = 1; $i -le $retries; $i++) {
        Start-Sleep -Seconds 2
        cmd /c "wsl -u root bash -lc `"systemctl is-active docker >/dev/null 2>&1; if [ -S /run/docker.sock ]; then exit 0; else exit 1; fi`" >nul 2>nul"
        if ($LASTEXITCODE -eq 0) {
            Write-Success "Docker daemon is running"
            break
        }
        if ($i -eq $retries) {
            Write-ErrorMsg "Docker daemon failed to start"
            Write-Host "Try running: wsl --shutdown"
            Write-Host "Then run this installer again"
            Write-Host ""
            Write-Host "Diagnostics:"
            Write-Host "  systemctl status docker (last 20 lines):"
            $diag = Invoke-WslRootCapture "systemctl status docker --no-pager -n 20 2>/dev/null || service docker status 2>/dev/null || true"
            if (-not [string]::IsNullOrWhiteSpace($diag)) { Write-Host $diag }
            Write-Host "  journalctl -u docker (last 50 lines):"
            $diag2 = Invoke-WslRootCapture "journalctl -u docker -n 50 --no-pager 2>/dev/null || true"
            if (-not [string]::IsNullOrWhiteSpace($diag2)) { Write-Host $diag2 }
            Invoke-Exit 1
        }
    }

    Ensure-DockerUserAccess
}

function Set-WSLNetworkingFixes {
    Write-Step "Applying WSL networking fixes for Docker registry connectivity..."

    Write-Host "Configuring IPv4 preference..."
    Invoke-WslRootCapture @'
set -e
if ! grep -qF "precedence ::ffff:0:0/96 100" /etc/gai.conf 2>/dev/null; then
  echo "precedence ::ffff:0:0/96 100" >> /etc/gai.conf
fi
'@
    Write-Host "IPv4 preference configured."

    Write-Host "Disabling IPv6 routing..."
    Invoke-WslRootCapture "sysctl -w net.ipv6.conf.all.disable_ipv6=1 >/dev/null 2>&1; sysctl -w net.ipv6.conf.default.disable_ipv6=1 >/dev/null 2>&1"
    Write-Host "IPv6 routing disabled."

    Invoke-WslRootCapture @'
set -e
if ! grep -q "^net.ipv6.conf.all.disable_ipv6=1" /etc/sysctl.conf 2>/dev/null; then
  echo "net.ipv6.conf.all.disable_ipv6=1" >> /etc/sysctl.conf
fi
if ! grep -q "^net.ipv6.conf.default.disable_ipv6=1" /etc/sysctl.conf 2>/dev/null; then
  echo "net.ipv6.conf.default.disable_ipv6=1" >> /etc/sysctl.conf
fi
'@
    Write-Host "IPv6 persistence configured."

    Write-Host "Configuring DNS..."
    Invoke-WslRootCapture @'
set -e
if ! grep -q "nameserver 8.8.8.8" /etc/resolv.conf 2>/dev/null; then
  cp /etc/resolv.conf /etc/resolv.conf.backup.$(date +%s) 2>/dev/null || true
  echo "nameserver 8.8.8.8" >> /etc/resolv.conf
fi
'@
    Write-Host "DNS configured."

    Write-Host "Configuring WSL to preserve DNS settings..."
    Invoke-WslRootCapture @'
set -e
if grep -q "generateResolvConf *= *false" /etc/wsl.conf 2>/dev/null; then
  exit 0
fi
if grep -q "generateResolvConf" /etc/wsl.conf 2>/dev/null; then
  sed -i "s/generateResolvConf *= *true/generateResolvConf = false/" /etc/wsl.conf
else
  if grep -q "^\[network\]" /etc/wsl.conf 2>/dev/null; then
    sed -i "/^\[network\]/a generateResolvConf = false" /etc/wsl.conf
  else
    printf "\n[network]\ngenerateResolvConf = false\n" >> /etc/wsl.conf
  fi
fi
'@
    Write-Host "WSL DNS persistence configured."

    Write-Success "Networking fixes applied"
}

function Test-DockerInstallation {
    Write-Step "Verifying Docker installation..."

    Write-Host "Running 'docker info' (root)..."
    $rootInfoOut = Invoke-WslRootCapture "docker info"
    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Docker daemon did not respond as root"
        if (-not [string]::IsNullOrWhiteSpace($rootInfoOut)) { Write-Host $rootInfoOut }
        return $false
    }
    Write-Success "Docker daemon is responsive (root check)"

    Write-Host "Testing Docker with hello-world image..."
    $pullOut = Invoke-WslRootCapture "if docker image inspect hello-world:latest >/dev/null 2>&1; then :; else docker pull hello-world:latest; fi"
    if (-not [string]::IsNullOrWhiteSpace($pullOut)) { Write-Host $pullOut }
    $runOut = Invoke-WslRootCapture "docker run --rm hello-world:latest"
    if ($LASTEXITCODE -ne 0) {
        Write-WarningMsg "Docker run test failed (root)."
        if (-not [string]::IsNullOrWhiteSpace($runOut)) { Write-Host $runOut }
        return $false
    }
    Write-Success "Docker run test passed"

    return $true
}

# ============================================================================
# Install Helpers
# ============================================================================

function ConvertTo-WSLPath {
    param([string]$WindowsPath)
    if ([string]::IsNullOrWhiteSpace($WindowsPath)) { return "" }
    try {
        $args = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $args += "-d"
            $args += $script:WslDistro
        }
        $args += "--"
        $args += "wslpath"
        $args += "-a"
        $args += $WindowsPath
        $out = & wsl.exe @args 2>$null
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($out)) {
            return $out.Trim()
        }
    }
    catch { }

    $p = $WindowsPath -replace '\\', '/'
    if ($p -match '^([A-Za-z]):(.*)$') {
        $drive = $Matches[1].ToLower()
        $rest = $Matches[2]
        return "/mnt/$drive$rest"
    }
    return $p
}

function ConvertFrom-WSLPath {
    param([string]$WslPath)
    if ([string]::IsNullOrWhiteSpace($WslPath)) { return "" }
    try {
        $args = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $args += "-d"
            $args += $script:WslDistro
        }
        $args += "--"
        $args += "wslpath"
        $args += "-w"
        $args += $WslPath
        $out = & wsl.exe @args 2>$null
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($out)) {
            return $out.Trim()
        }
    }
    catch { }

    if ($WslPath -match '^/mnt/([a-z])/(.*)') {
        $drive = $Matches[1].ToUpper()
        $rest = $Matches[2] -replace '/', '\'
        return "${drive}:\$rest"
    }
    return $WslPath
}

function Append-EnvLine {
    param(
        [string]$EnvFilePath,
        [string]$Line
    )

    $tempPath = Join-Path $env:TEMP ("pf-env-" + [guid]::NewGuid().ToString("N") + ".txt")
    $stdoutPath = Join-Path $env:TEMP ("pf-env-out-" + [guid]::NewGuid().ToString("N") + ".log")
    $stderrPath = Join-Path $env:TEMP ("pf-env-err-" + [guid]::NewGuid().ToString("N") + ".log")
    try {
        [System.IO.File]::WriteAllText($tempPath, ($Line + "`n"), [System.Text.UTF8Encoding]::new($false))

        $wslArgs = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $wslArgs += "-d"
            $wslArgs += $script:WslDistro
        }
        $wslArgs += "-u"
        $wslArgs += "root"
        $wslArgs += "--exec"
        $wslArgs += "/usr/bin/tee"
        $wslArgs += "-a"
        $wslArgs += "--"
        $wslArgs += $EnvFilePath

        $proc = Start-Process -FilePath "wsl.exe" -ArgumentList $wslArgs -RedirectStandardInput $tempPath -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -NoNewWindow -Wait -PassThru
        if ($proc.ExitCode -ne 0) {
            $stderr = ""
            $stdout = ""
            if (Test-Path -LiteralPath $stderrPath) { $stderr = (Get-Content -LiteralPath $stderrPath -Raw) }
            if (Test-Path -LiteralPath $stdoutPath) { $stdout = (Get-Content -LiteralPath $stdoutPath -Raw) }
            $details = ($stderr + "`n" + $stdout).Trim()
            if ([string]::IsNullOrWhiteSpace($details)) {
                throw "Failed to append env line (exit code $($proc.ExitCode))."
            }
            throw "Failed to append env line (exit code $($proc.ExitCode)). $details"
        }
    }
    finally {
        if (Test-Path -LiteralPath $tempPath) { Remove-Item -LiteralPath $tempPath -Force }
        if (Test-Path -LiteralPath $stdoutPath) { Remove-Item -LiteralPath $stdoutPath -Force }
        if (Test-Path -LiteralPath $stderrPath) { Remove-Item -LiteralPath $stderrPath -Force }
    }
}

function Find-PreviousInstallDir {
    $mountPath = Invoke-WslRootCapture "wslpath -m / 2>/dev/null"
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($mountPath)) {
        return $null
    }
    $sedCmd = "echo '$mountPath' | sed 's|/$DEPLOYMENT_DIR.*$||' || true"
    $installDir = Invoke-WslRootCapture $sedCmd
    if (-not [string]::IsNullOrWhiteSpace($installDir)) {
        Write-Success "Found previous installation at: $installDir"
        return $installDir.Trim()
    }
    return $null
}

function Set-InstallDirectory {
    param([string]$DefaultDir)

    $previousDir = Find-PreviousInstallDir
    $suggestedDir = if ($previousDir) { $previousDir } else { $DefaultDir }

    Write-Host ""
    Write-Host "Suggested installation location: $suggestedDir"

    if ($Force -or $Silent) {
        $useIt = $true
    }
    else {
        $useIt = Read-YesNoPrompt "Use this location?" -DefaultYes:$true
    }

    if (-not $useIt) {
        if ($Silent) {
            Write-ErrorMsg "InstallDir not provided in Silent mode."
            Invoke-Exit 1
        }
        $customDir = Read-HostLine "Enter installation directory [$DefaultDir]"
        if ([string]::IsNullOrWhiteSpace($customDir)) {
            return (Resolve-WslPath -Path $DefaultDir)
        }
        return (Resolve-WslPath -Path $customDir)
    }

    $resolved = Resolve-WslPath -Path $suggestedDir
    if ($env:PROTOFLEET_DEBUG -eq "1") {
        Write-Host ("DEBUG: Resolved install dir '{0}' -> '{1}'" -f $suggestedDir, $resolved)
    }
    return $resolved
}

function Copy-ToWSL {
    param(
        [string]$WindowsFilePath,
        [string]$WSLTempPath
    )

    Write-Step "Transferring to WSL..."

    try {
        if (-not (Test-Path -LiteralPath $WindowsFilePath)) {
            throw "Windows file not found: $WindowsFilePath"
        }

        $targetDir = Split-Path -Parent $WSLTempPath
        if ([string]::IsNullOrWhiteSpace($targetDir)) { $targetDir = "/tmp" }

        Invoke-WslExec -Executable "/bin/mkdir" -Arguments @("-p", $targetDir) -Root | Out-Null

        $wslArgs = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $wslArgs += "-d"
            $wslArgs += $script:WslDistro
        }
        $wslArgs += "-u"
        $wslArgs += "root"
        $wslArgs += "--exec"
        $wslArgs += "/usr/bin/tee"
        $wslArgs += "--"
        $wslArgs += $WSLTempPath

        if ($env:PROTOFLEET_DEBUG -eq "1") {
            Write-Host "DEBUG: Copy-ToWSL streaming file to WSL"
            Write-Host ("DEBUG: wsl.exe " + ($wslArgs -join " "))
        }

        $stdoutPath = Join-Path $env:TEMP ("pf-wsl-copy-out-" + [guid]::NewGuid().ToString("N") + ".log")
        $stderrPath = Join-Path $env:TEMP ("pf-wsl-copy-err-" + [guid]::NewGuid().ToString("N") + ".log")
        try {
            $proc = Start-Process -FilePath "wsl.exe" -ArgumentList $wslArgs -RedirectStandardInput $WindowsFilePath -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -NoNewWindow -Wait -PassThru
            if ($proc.ExitCode -ne 0) {
                $stderr = ""
                $stdout = ""
                if (Test-Path -LiteralPath $stderrPath) { $stderr = (Get-Content -LiteralPath $stderrPath -Raw) }
                if (Test-Path -LiteralPath $stdoutPath) { $stdout = (Get-Content -LiteralPath $stdoutPath -Raw) }
                $details = ($stderr + "`n" + $stdout).Trim()
                if ([string]::IsNullOrWhiteSpace($details)) {
                    throw "Copy to WSL failed with exit code $($proc.ExitCode)."
                }
                throw "Copy to WSL failed with exit code $($proc.ExitCode). $details"
            }
        }
        finally {
            if (Test-Path -LiteralPath $stdoutPath) { Remove-Item -LiteralPath $stdoutPath -Force }
            if (Test-Path -LiteralPath $stderrPath) { Remove-Item -LiteralPath $stderrPath -Force }
        }

        Write-Success "Transferred to WSL: $WSLTempPath"
    }
    catch {
        $msg = $_.Exception.Message
        if ([string]::IsNullOrWhiteSpace($msg)) { $msg = $_.ToString() }
        Write-ErrorMsg "Failed to transfer to WSL: $msg"
        Write-Host "WSL may not be initialized or the target distro may not be ready."
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            Write-Host "Try running 'wsl.exe -d $($script:WslDistro)' once to complete setup, then retry."
        }
        else {
            Write-Host "Try running 'wsl.exe' once to complete setup, then retry."
        }
        Invoke-Exit 1
    }
}

function Expand-InWSL {
    param(
        [string]$TarPath,
        [string]$TargetDir
    )

    Write-Step "Extracting to $TargetDir..."

    Invoke-WslExec -Executable "/bin/mkdir" -Arguments @("-p", $TargetDir) -Root | Out-Null

    $envFile = "$TargetDir/$DEPLOYMENT_DIR/server/influx_config/.env"
    $checkEnvCmd = "[ -f '$envFile' ] && echo 'yes' || echo 'no'"
    $preserveEnv = Invoke-WslRootCapture $checkEnvCmd

    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        if ($preserveEnv -eq "yes") {
            Write-Host "Preserving existing InfluxDB config .env file"
            Invoke-Wsl -Command "tar --warning=no-unknown-keyword -xzf '$TarPath' -C '$TargetDir' --exclude='${DEPLOYMENT_DIR}/server/influx_config/.env'" -Root
        }
        else {
            Invoke-Wsl -Command "tar --warning=no-unknown-keyword -xzf '$TarPath' -C '$TargetDir'" -Root
        }
    }
    finally {
        $ErrorActionPreference = $prev
    }

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to extract tarball"
        Invoke-Exit 1
    }

    Invoke-WslExec -Executable "/bin/rm" -Arguments @("--", $TarPath) -Root | Out-Null

    Write-Success "Extraction complete"

    return "$TargetDir/$DEPLOYMENT_DIR"
}

function Copy-DeploymentIntoWSL {
    param(
        [string]$WindowsDeploymentPath,
        [string]$TargetDir
    )

    Write-Step "Copying deployment into WSL..."
    $wslTargetDir = if ([string]::IsNullOrWhiteSpace($TargetDir)) { "~/proto-fleet" } else { $TargetDir }
    $wslTargetDir = Resolve-WslPath -Path $wslTargetDir

    if (-not (Test-Path -LiteralPath $WindowsDeploymentPath)) {
        Write-ErrorMsg "Windows deployment path not found: $WindowsDeploymentPath"
        Invoke-Exit 1
    }

    $wslTempPath = "/tmp/proto-fleet-deployment.tar.gz"
    $parentDir = Split-Path -Parent $WindowsDeploymentPath
    $baseName = Split-Path -Leaf $WindowsDeploymentPath
    $tarPath = Join-Path $parentDir "${baseName}.tar.gz"

    try {
        if (Test-Path -LiteralPath $tarPath) { Remove-Item -LiteralPath $tarPath -Force }
        tar -czf $tarPath -C $parentDir $baseName
    }
    catch {
        Write-ErrorMsg "Failed to create deployment archive: $($_.Exception.Message)"
        Invoke-Exit 1
    }

    Copy-ToWSL -WindowsFilePath $tarPath -WSLTempPath $wslTempPath
    try { Remove-Item -LiteralPath $tarPath -Force } catch { }

    Invoke-WslExec -Executable "/bin/mkdir" -Arguments @("-p", $wslTargetDir) -Root | Out-Null
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        Invoke-Wsl -Command "tar --warning=no-unknown-keyword -xzf '$wslTempPath' -C '$wslTargetDir'" -Root
    }
    finally {
        $ErrorActionPreference = $prev
    }
    Invoke-WslExec -Executable "/bin/rm" -Arguments @("--", $wslTempPath) -Root | Out-Null

    $deploymentPath = "$wslTargetDir/$baseName"
    Write-Success "Deployment copied into WSL: $deploymentPath"
    return $deploymentPath
}

function Test-PluginBinaries {
    param([string]$DeploymentPath)

    Write-Step "Validating plugin binaries..."

    $serverPath = "$DeploymentPath/server"
    $serverExists = Test-WslPathExists -Path $serverPath -Directory -Root
    if ($env:PROTOFLEET_DEBUG -eq "1") {
        Write-Host "DEBUG: Test-WslPathExists dir $serverPath => $serverExists"
    }
    if (-not $serverExists) {
        $winPath = ConvertFrom-WSLPath $DeploymentPath
        Write-WarningMsg "WSL cannot access the deployment path: $DeploymentPath"
        if ($winPath -and (Test-Path -LiteralPath (Join-Path $winPath "server"))) {
            Write-Host "The files exist on Windows at: $winPath"
            Write-Host "Attempting to copy the deployment into WSL..."
            $targetDir = "~/proto-fleet"
            $deploymentPath = Copy-DeploymentIntoWSL -WindowsDeploymentPath $winPath -TargetDir $targetDir
            $serverPath = "$deploymentPath/server"
            $serverExists = Test-WslPathExists -Path $serverPath -Directory -Root
            if ($env:PROTOFLEET_DEBUG -eq "1") {
                Write-Host "DEBUG: Test-WslPathExists dir $serverPath => $serverExists"
            }
            if (-not $serverExists) {
                Write-ErrorMsg "Failed to access the copied deployment inside WSL."
                Invoke-Exit 1
            }
        }
        else {
            Write-Host "Please verify the deployment path is correct and accessible from WSL."
            Invoke-Exit 1
        }
    }

    $missingPlugins = @()

    foreach ($plugin in $REQUIRED_PLUGINS) {
        $pluginPath = "$serverPath/$plugin"
        $exists = Test-WslPathExists -Path $pluginPath -File -Root
        if ($env:PROTOFLEET_DEBUG -eq "1") {
            Write-Host "DEBUG: Test-WslPathExists file $pluginPath => $exists"
        }
        if (-not $exists) {
            $missingPlugins += $plugin
        }
    }

    if ($missingPlugins.Count -gt 0) {
        Write-ErrorMsg "Missing plugin binaries:"
        foreach ($plugin in $missingPlugins) {
            Write-Host "  - $plugin" -ForegroundColor Red
        }
        Write-Host ""
        $winPath = ConvertFrom-WSLPath $DeploymentPath
        if ($winPath -and (Test-Path -LiteralPath (Join-Path $winPath "server"))) {
            Write-Host "Windows path exists: $winPath"
            Write-Host "WSL may not be seeing Windows mounts. Ensure WSL automount is enabled or move the deployment into WSL."
        }
        Write-Host "The installation package may be incomplete. Please contact support."
        Invoke-Exit 1
    }

    if ($DeploymentPath -match '^/mnt/') {
        Write-WarningMsg "Plugin binaries are on a Windows-mounted path; chmod is not supported there. Skipping chmod."
    }
    else {
        Invoke-Wsl -Command "chmod +x '$serverPath'/*-plugin-*" -Root
    }

    Write-Success "Plugin binaries validated"
}

function Test-EnvFileComplete {
    param([string]$EnvFilePath)

    $requiredKeys = @(
        "MYSQL_ROOT_PASSWORD",
        "DB_USERNAME",
        "DB_PASSWORD",
        "AUTH_CLIENT_SECRET_KEY",
        "ENCRYPT_SERVICE_MASTER_KEY"
    )

    $allPresent = $true

    foreach ($key in $requiredKeys) {
        $check = Invoke-WslExec -Executable "/bin/grep" -Arguments @("-q", "^$key=", $EnvFilePath) -Root -Quiet
        if ($check.ExitCode -ne 0) {
            Write-Host "Missing required key: $key" -ForegroundColor Red
            $allPresent = $false
        }
    }

    return $allPresent
}

function New-RandomPassword {
    param([int]$Length = 24)

    $result = Invoke-WslRootCapture "openssl rand -base64 $Length"
    return $result.Trim()
}

function New-Base64Key {
    param([int]$Bytes = 32)

    $result = Invoke-WslRootCapture "openssl rand -base64 $Bytes"
    return $result.Trim()
}

function Invoke-MySQLVolumePrompt {
    param([string]$DeploymentPath)

    $baseNameCmd = "basename '$DeploymentPath' | sed 's|/$DEPLOYMENT_DIR$||'"
    $projectName = Invoke-WslRootCapture $baseNameCmd
    $volumeCmd = "docker volume ls -q | grep -E '^${projectName}[-_]mysql$' || true"
    $volumeName = Invoke-WslRootCapture $volumeCmd

    if (-not [string]::IsNullOrWhiteSpace($volumeName)) {
        Write-Host ""
        Write-WarningMsg "Detected existing MySQL data volume: $volumeName"
        Write-Host ""

        if ($Force -or $Silent) {
            $remove = $false
        }
        else {
            $remove = Read-YesNoPrompt "Remove & reinitialize this volume now? ALL DATA WILL BE LOST" -DefaultYes:$false
        }

        if ($remove) {
            Write-Host "Shutting down containers..."
            Invoke-Wsl -Command "cd '$DeploymentPath' && docker compose -f docker-compose.yaml down" -Root

            Write-Host "Removing volume $volumeName..."
            Invoke-Wsl -Command "docker volume rm '$volumeName'" -Root

            Write-Success "Volume removed; new credentials will apply on next startup"
        }
        else {
            Write-WarningMsg "Keeping existing MySQL data. New credentials will NOT be applied."
            Write-Host "If you want to use new credentials, run this installer again and choose to remove the volume."
        }
    }
}

function New-EnvironmentFile {
    param([string]$DeploymentPath)

    Write-Step "Configuring Proto Fleet..."

    $envFile = "$DeploymentPath/.env"

    $existingEnvFile = Test-WslPathExists -Path $envFile -File -Root

    if ($existingEnvFile) {
        if (Test-EnvFileComplete -EnvFilePath $envFile) {
            Write-Block @(
                "Existing environment file found with all required keys.",
                "Use it to keep the current configuration and data (recommended if you're upgrading).",
                "Choose No for a clean install and to regenerate credentials."
            )

            if ($Force -or $Silent) {
                $useExisting = $true
            }
            else {
                Write-Host ""
                $useExisting = Read-YesNoPrompt "Use existing configuration? (Yes = keep current, No = clean install + new credentials)" -DefaultYes:$true
            }

            if ($useExisting) {
                Write-Success "Using existing environment file"
                Ensure-EnvFileOwnership -EnvFilePath $envFile
                return $envFile
            }

            Invoke-MySQLVolumePrompt -DeploymentPath $DeploymentPath
        }
    }

    Write-Host ""
    $credentialOptions = @(
        @{ Value = "auto"; Label = "Auto-generate (Recommended)"; Description = "Creates strong random passwords and keys. Most users should choose this." },
        @{ Value = "custom"; Label = "Custom credentials"; Description = "You enter each password and key manually." }
    )

    if ($script:SimpleSetup -or $Force -or $Silent) {
        $choice = "auto"
    }
    else {
        $choice = Read-ChoicePrompt -Prompt "How would you like to configure backend credentials?" -Options $credentialOptions -DefaultValue "auto"
        if ([string]::IsNullOrWhiteSpace($choice)) { $choice = "auto" }
    }

    $touchResult = Invoke-WslExec -Executable "/usr/bin/touch" -Arguments @("--", $envFile) -Root
    if ($touchResult.ExitCode -ne 0) {
        Write-ErrorMsg "Failed to create env file: $envFile"
        if ($touchResult.Output) { Write-Host $touchResult.Output }
        Invoke-Exit 1
    }

    if ($choice -eq "custom") {
        if ($Silent) {
            Write-ErrorMsg "Custom password entry is not available in Silent mode."
            Invoke-Exit 1
        }

        Write-Host ""
        $mysqlRootPass = Read-SecureInput "Enter password for Database root user"
        Append-EnvLine -EnvFilePath $envFile -Line "MYSQL_ROOT_PASSWORD=$mysqlRootPass"

        $dbUsername = Read-HostLine "Enter username for Database user [fleet_user]"
        if ([string]::IsNullOrWhiteSpace($dbUsername)) {
            $dbUsername = "fleet_user"
        }
        Append-EnvLine -EnvFilePath $envFile -Line "DB_USERNAME=$dbUsername"

        $dbPassword = Read-SecureInput "Enter password for Database user"
        Append-EnvLine -EnvFilePath $envFile -Line "DB_PASSWORD=$dbPassword"

        Write-Host ""
        Write-Host "Auth client secret key (minimum 32 characters):"
        $authKey = Read-SecureInput "Enter Auth client secret key"
        while ($authKey.Length -lt 32) {
            Write-WarningMsg "Secret key must be at least 32 characters long (current: $($authKey.Length))"
            $authKey = Read-SecureInput "Enter Auth client secret key"
        }
        Append-EnvLine -EnvFilePath $envFile -Line "AUTH_CLIENT_SECRET_KEY=$authKey"

        Write-Host ""
        Write-Host "Encryption service master key (must be 32-byte Base64-encoded):"
        $encryptKey = Read-SecureInput "Enter Encryption service master key"

        $validateKeyCmd = 'validate_key() { local input="$1"; local temp_file=$(mktemp); if ! echo "$input" | base64 -d > "$temp_file" 2>/dev/null; then rm -f "$temp_file"; return 1; fi; local byte_length=$(wc -c < "$temp_file"); rm -f "$temp_file"; if [ "$byte_length" -ne 32 ]; then return 2; fi; return 0; } validate_key ' + "'" + $encryptKey + "'" + ' && echo valid || echo invalid'
        $valid = Invoke-WslRootCapture $validateKeyCmd

        while ($valid -ne "valid") {
            Write-WarningMsg "The provided key is not valid Base64 or doesn't decode to 32 bytes"
            $encryptKey = Read-SecureInput "Enter Encryption service master key"
            $validateKeyCmd = 'validate_key() { local input="$1"; local temp_file=$(mktemp); if ! echo "$input" | base64 -d > "$temp_file" 2>/dev/null; then rm -f "$temp_file"; return 1; fi; local byte_length=$(wc -c < "$temp_file"); rm -f "$temp_file"; if [ "$byte_length" -ne 32 ]; then return 2; fi; return 0; } validate_key ' + "'" + $encryptKey + "'" + ' && echo valid || echo invalid'
            $valid = Invoke-WslRootCapture $validateKeyCmd
        }

        Append-EnvLine -EnvFilePath $envFile -Line "ENCRYPT_SERVICE_MASTER_KEY=$encryptKey"
    }
    else {
        Write-Host ""
        Write-Host "Generating secure backend credentials..."

        $mysqlRootPass = New-RandomPassword
        $dbUsername = "fleet_user"
        $dbPassword = New-RandomPassword
        $authKey = New-Base64Key
        $encryptKey = New-Base64Key

        Append-EnvLine -EnvFilePath $envFile -Line "MYSQL_ROOT_PASSWORD=$mysqlRootPass"
        Append-EnvLine -EnvFilePath $envFile -Line "DB_USERNAME=$dbUsername"
        Append-EnvLine -EnvFilePath $envFile -Line "DB_PASSWORD=$dbPassword"
        Append-EnvLine -EnvFilePath $envFile -Line "AUTH_CLIENT_SECRET_KEY=$authKey"
        Append-EnvLine -EnvFilePath $envFile -Line "ENCRYPT_SERVICE_MASTER_KEY=$encryptKey"

        Write-Success "Generated secure backend credentials"
    }

    Ensure-EnvFileOwnership -EnvFilePath $envFile
    Write-Success "Environment configuration saved to $envFile"

    return $envFile
}

function New-SelfSignedCertificate {
    param([string]$DeploymentPath)

    Write-Host "Generating self-signed SSL certificate..."

    $sslDir = "$DeploymentPath/ssl"
    $sslCert = "$sslDir/cert.pem"
    $sslKey = "$sslDir/key.pem"

    Invoke-WslExec -Executable "/bin/mkdir" -Arguments @("-p", $sslDir) -Root | Out-Null

    $localIps = Invoke-WslRootCapture "hostname -I 2>/dev/null | tr ' ' '\n' | grep -v '^127\.' | tr '\n' ' '"
    $hostname = Invoke-WslRootCapture "hostname"

    $sanEntries = "DNS:localhost,IP:127.0.0.1,IP:::1"

    if (-not [string]::IsNullOrWhiteSpace($hostname)) {
        $hostnameClean = $hostname.Trim()
        $sanEntries += ",DNS:$hostnameClean,DNS:${hostnameClean}.local"
    }

    if (-not [string]::IsNullOrWhiteSpace($localIps)) {
        foreach ($ip in ($localIps -split '\s+')) {
            if (-not [string]::IsNullOrWhiteSpace($ip)) {
                $sanEntries += ",IP:$ip"
            }
        }
    }

    Write-Host "Certificate will be valid for: $sanEntries"

    $opensslCmd = "openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout '$sslKey' -out '$sslCert' -subj '/C=US/ST=Local/L=Local/O=ProtoFleet/CN=localhost' -addext 'subjectAltName=$sanEntries' 2>&1"

    $result = Invoke-WslRootCapture $opensslCmd

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to generate SSL certificate"
        Write-Host $result
        return $false
    }

    Invoke-WslExec -Executable "/bin/chmod" -Arguments @("600", $sslKey) -Root | Out-Null
    Invoke-WslExec -Executable "/bin/chmod" -Arguments @("644", $sslCert) -Root | Out-Null

    Write-Success "Self-signed certificate generated successfully"
    Write-Host ""
    Write-Host "NOTE: Browsers will show a security warning for self-signed certificates."
    Write-Host "      You can accept the warning to proceed, or import the certificate"
    Write-Host "      into your browser/OS trust store."

    return $true
}

function Set-SSLConfiguration {
    param([string]$DeploymentPath)

    Write-Step "Configuring SSL/TLS..."

    $sslDir = "$DeploymentPath/ssl"
    $sslCert = "$sslDir/cert.pem"
    $sslKey = "$sslDir/key.pem"

    Invoke-WslExec -Executable "/bin/mkdir" -Arguments @("-p", $sslDir) -Root | Out-Null

    $checkCertCmd = "[ -f '$sslCert' ] && [ -f '$sslKey' ] && echo 'yes' || echo 'no'"
    $certExists = Invoke-WslRootCapture $checkCertCmd

    $protocolMode = "http"

    if ($script:SimpleSetup) {
        Write-Block @(
            "Using HTTP mode (no encryption)."
        )
        $protocolMode = "http"
    }
    elseif ($certExists -eq "yes") {
        Write-Block @(
            "Found existing SSL certificates in $sslDir",
            "Certificate: $sslCert",
            "Private Key: $sslKey"
        )
        $protocolMode = "https"
    }
    else {
        $sslOptions = @(
            @{ Value = "http"; Label = "HTTP only (Recommended for internal LAN)"; Description = "No encryption. Simplest for private networks." },
            @{ Value = "selfsigned"; Label = "HTTPS with self-signed certificate"; Description = "Generates a certificate automatically. Browsers will show a warning." },
            @{ Value = "owncerts"; Label = "HTTPS with your own certificates"; Description = "Provide cert.pem and key.pem. The installer will help you place and verify them." }
        )

        $protocolMode = $null
        while (-not $protocolMode) {
            if ($Force -or $Silent) {
                $sslChoice = "http"
            }
            else {
                $sslChoice = Read-ChoicePrompt -Prompt "No SSL certificates found. Choose how to configure HTTPS:" -Options $sslOptions -DefaultValue "http"
                if ([string]::IsNullOrWhiteSpace($sslChoice)) { $sslChoice = "http" }
            }

            switch ($sslChoice) {
                "selfsigned" {
                    if (New-SelfSignedCertificate -DeploymentPath $DeploymentPath) {
                        $protocolMode = "https"
                    }
                    else {
                        Write-WarningMsg "Falling back to HTTP mode"
                        $protocolMode = "http"
                    }
                }
                "owncerts" {
                    $ok = Handle-UserProvidedCerts -DeploymentPath $DeploymentPath
                    if ($ok) {
                        $protocolMode = "https"
                    }
                    else {
                        if ($Silent) {
                            Write-ErrorMsg "TLS certificates not provided in Silent mode."
                            Invoke-Exit 1
                        }
                        Write-WarningMsg "TLS certificates were not provided. Choose another option."
                    }
                }
                default {
                    Write-Block @(
                        "Using HTTP mode (no encryption)."
                    )
                    $protocolMode = "http"
                }
            }
        }
    }

    Write-Block @(
        "Protocol mode: $protocolMode"
    )

    $nginxSrc = "$DeploymentPath/client/nginx.$protocolMode.conf"
    $nginxDest = "$DeploymentPath/client/nginx.conf"

    Invoke-Wsl -Command "cp '$nginxSrc' '$nginxDest'" -Root

    if ($LASTEXITCODE -ne 0) {
        Write-ErrorMsg "Failed to copy nginx configuration"
        Invoke-Exit 1
    }

    $envFile = "$DeploymentPath/.env"
    $cookieSecure = if ($protocolMode -eq "https") { "true" } else { "false" }

    $checkSettingCmd = "grep -q '^SESSION_COOKIE_SECURE=' '$envFile' && echo 'yes' || echo 'no'"
    $hasSettingResult = Invoke-WslRootCapture $checkSettingCmd

    if ($hasSettingResult -eq "yes") {
        Invoke-Wsl -Command "sed -i 's/^SESSION_COOKIE_SECURE=.*/SESSION_COOKIE_SECURE=$cookieSecure/' '$envFile'" -Root
    }
    else {
        Append-EnvLine -EnvFilePath $envFile -Line "SESSION_COOKIE_SECURE=$cookieSecure"
    }

    Write-Success "SSL/TLS configuration complete"

    return $protocolMode
}

function Write-HostLines {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    ($Text -replace "`r", "`n").Split("`n") | ForEach-Object {
        if ($_ -ne "") { Write-Host $_ }
    }
}

function Write-DockerTlsGuidance {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    if ($Text -match "tls: bad record MAC" -or $Text -match "failed to compute cache key") {
        Write-WarningMsg "Detected a transient TLS error while talking to the registry (Docker engine is running in WSL)."
        Write-Host "Common causes: flaky network/VPN/proxy, a WSL/Docker glitch, or system time drift."
        Write-Host "We will try a quick WSL reset before the next retry. If it persists, disable VPN/proxy and retry."
    }
}

function Write-DockerDnsGuidance {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return }
    if ($Text -match "server misbehaving" -or $Text -match "lookup registry-1.docker.io on 127.0.0.53") {
        Write-WarningMsg "Detected a DNS resolver issue inside WSL (127.0.0.53)."
        Write-Host "We will apply a WSL DNS fix (static resolv.conf) and restart WSL before retrying."
    }
}

function Ensure-WslDnsFix {
    Write-Host "Applying WSL DNS fix..."
    Invoke-WslRootCapture @"
set -e
if [ -L /etc/resolv.conf ]; then
  rm -f /etc/resolv.conf
fi
cat > /etc/resolv.conf <<'EOF'
nameserver 1.1.1.1
nameserver 8.8.8.8
EOF
if [ -f /etc/wsl.conf ]; then
  if grep -q "generateResolvConf" /etc/wsl.conf; then
    sed -i "s/generateResolvConf *= *true/generateResolvConf = false/" /etc/wsl.conf
  elif grep -q "^\[network\]" /etc/wsl.conf; then
    sed -i "/^\[network\]/a generateResolvConf = false" /etc/wsl.conf
  else
    printf "\n[network]\ngenerateResolvConf = false\n" >> /etc/wsl.conf
  fi
else
  printf "[network]\ngenerateResolvConf = false\n" > /etc/wsl.conf
fi
"@
    try { & wsl.exe --shutdown | Out-Null } catch {}
    Start-Sleep -Seconds 2
}

function Try-WslReset {
    Write-Host "Resetting WSL to clear Docker engine state..."
    try {
        & wsl.exe --shutdown | Out-Null
        Start-Sleep -Seconds 5
        return $true
    }
    catch {
        Write-WarningMsg "WSL reset failed: $($_.Exception.Message)"
        return $false
    }
}

function Try-WslRestartDocker {
    Write-Host "Restarting Docker engine inside WSL..."
    try {
        $restartCmd = "systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || /etc/init.d/docker restart 2>/dev/null || true"
        $args = @()
        if (-not [string]::IsNullOrWhiteSpace($script:WslDistro)) {
            $args += "-d"
            $args += $script:WslDistro
        }
        $args += @("-u", "root", "bash", "-lc", $restartCmd)

        $proc = Start-Process -FilePath "wsl.exe" -ArgumentList $args -PassThru -WindowStyle Hidden
        if (-not $proc.WaitForExit(30000)) {
            try { $proc.Kill() | Out-Null } catch {}
            Write-WarningMsg "Docker restart timed out"
            return $false
        }
        return ($proc.ExitCode -eq 0)
    }
    catch {
        Write-WarningMsg "Docker restart in WSL failed: $($_.Exception.Message)"
        return $false
    }
}

function Start-DockerCompose {
    param([string]$DeploymentPath)

    Write-Step "Deploying Proto Fleet with Docker Compose..."

    $arch = Invoke-WslRootCapture "uname -m"
    $targetArch = if ($arch -match "arm64|aarch64") { "arm64" } else { "amd64" }

    Write-Host "Detected architecture: $arch (using TARGETARCH=$targetArch)"

    Write-Host "Pulling Docker images..."
    $pullOk = Invoke-WithRetryStream -ActionDescription "Docker image pull" -Operation {
        Invoke-WslCommandWithSpinner -Command "cd '$DeploymentPath' && DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml pull" -Activity "Pulling Docker images"
    } -OnFailure {
        Write-Host "Diagnostic output from docker pull:"
        try {
            if (-not [string]::IsNullOrWhiteSpace($script:LastCommandOutput)) {
                $diag = $script:LastCommandOutput
            }
            else {
                $diag = Invoke-WslRootCapture "cd '$DeploymentPath' && DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml pull | sed -u 's/\r/\n/g'"
            }
            Write-HostLines $diag
            Write-DockerTlsGuidance $diag
            Write-DockerDnsGuidance $diag
            if ($diag -match "tls: bad record MAC" -or $diag -match "failed to compute cache key") {
                Try-WslReset | Out-Null
                Try-WslRestartDocker | Out-Null
            }
            if ($diag -match "server misbehaving" -or $diag -match "lookup registry-1.docker.io on 127.0.0.53") {
                Ensure-WslDnsFix | Out-Null
            }
        }
        catch {
            $msg = $_.Exception.ToString()
            Write-HostLines $msg
            Write-DockerTlsGuidance $msg
            Write-DockerDnsGuidance $msg
            if ($msg -match "tls: bad record MAC" -or $msg -match "failed to compute cache key") {
                Try-WslReset | Out-Null
                Try-WslRestartDocker | Out-Null
            }
            if ($msg -match "server misbehaving" -or $msg -match "lookup registry-1.docker.io on 127.0.0.53") {
                Ensure-WslDnsFix | Out-Null
            }
        }
    }
    Reset-ConsoleLine

    if (-not $pullOk) {
        Write-Host "No live output captured. Running a diagnostic pull to capture errors..."
        $diag = Invoke-WslRootCapture "cd '$DeploymentPath' && DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml pull | sed -u 's/\r/\n/g'"
        Write-HostLines $diag
        Write-DockerTlsGuidance $diag
        Write-DockerDnsGuidance $diag
        Write-ErrorMsg "Failed to pull Docker images"
        Invoke-Exit 1
    }

    Write-Host "Building Docker images (this may take several minutes)..."
    $buildOk = Invoke-WithRetryStream -ActionDescription "Docker image build" -Operation {
        Invoke-WslCommandWithSpinner -Command "cd '$DeploymentPath' && export TARGETARCH='$targetArch' && DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml build --no-cache" -Activity "Building Docker images"
    } -OnFailure {
        Write-Host "Diagnostic output from docker build:"
        if (-not [string]::IsNullOrWhiteSpace($script:LastCommandOutput)) {
            $diag = $script:LastCommandOutput
        }
        else {
            $diag = Invoke-WslRootCapture "cd '$DeploymentPath' && export TARGETARCH='$targetArch' && DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml build --no-cache | sed -u 's/\r/\n/g'"
        }
        Write-HostLines $diag
        Write-DockerTlsGuidance $diag
        if ($diag -match "tls: bad record MAC" -or $diag -match "failed to compute cache key") {
            Try-WslReset | Out-Null
            Try-WslRestartDocker | Out-Null
        }
        if ($diag -match "Image deployment-fleet-api Built" -and $diag -match "Image deployment-fleet-client Built" -and $diag -notmatch "error|failed|denied|tls") {
            Write-WarningMsg "Build appears successful despite non-zero exit code. Continuing."
            return $true
        }
    }
    Reset-ConsoleLine

    if (-not $buildOk) {
        Write-ErrorMsg "Docker build failed"
        Invoke-Exit 1
    }

    Write-Host "Stopping any running services..."
    $downResult = Invoke-WslCommandWithSpinner -Command "cd '$DeploymentPath' && docker compose -f docker-compose.yaml down" -Activity "Stopping services" -TimeoutSeconds 300
    Reset-ConsoleLine

    Write-Host "Starting services..."
    $upResult = Invoke-WslCommandWithSpinner -Command "cd '$DeploymentPath' && docker compose -f docker-compose.yaml up -d" -Activity "Starting services" -TimeoutSeconds 300
    Reset-ConsoleLine

    if ($upResult.ExitCode -ne 0) {
        if (-not [string]::IsNullOrWhiteSpace($script:LastCommandOutput)) {
            Write-HostLines $script:LastCommandOutput
        }
        Write-ErrorMsg "Failed to start services"
        Invoke-Exit 1
    }

    Write-Success "Services started successfully"
}

function Wait-ForHealthyServices {
    param([string]$DeploymentPath)

    Write-Step "Waiting for services to become healthy..."

    $maxWait = 60
    $elapsed = 0

    while ($elapsed -lt $maxWait) {
        Start-Sleep -Seconds 2
        $elapsed += 2

        $statusCmd = "cd '$DeploymentPath' && docker compose ps --format json 2>/dev/null || echo '[]'"
        $status = Invoke-WslRootCapture $statusCmd

        if ($status -ne "[]") {
            $runningCmd = "cd '$DeploymentPath' && docker compose ps --format '{{.State}}' 2>/dev/null | grep -c 'running' || echo '0'"
            $runningCount = Invoke-WslRootCapture $runningCmd

            if ([int]$runningCount -gt 0) {
                Write-Success "Services are running"
                return $true
            }
        }

        Write-Host "  Waiting... ($elapsed / $maxWait seconds)"
    }

    Write-WarningMsg "Services may still be starting up"
    return $false
}

function Test-DeploymentRoot {
    param([string]$Path)

    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }

    $compose = Join-Path $Path "docker-compose.yaml"
    $serverDir = Join-Path $Path "server"
    $clientDir = Join-Path $Path "client"

    return (Test-Path -LiteralPath $compose -PathType Leaf) -and
           (Test-Path -LiteralPath $serverDir -PathType Container) -and
           (Test-Path -LiteralPath $clientDir -PathType Container)
}

function Test-DeploymentRootOrSubdir {
    param([string]$Path)

    if (Test-DeploymentRoot -Path $Path) { return $Path }

    $sub = Join-Path $Path $DEPLOYMENT_DIR
    if ((Test-Path -LiteralPath $sub -PathType Container) -and (Test-DeploymentRoot -Path $sub)) {
        return $sub
    }

    return $null
}

function Find-DeploymentRoot {
    param(
        [string]$StartDir,
        [int]$MaxDepth = 12
    )

    if ([string]::IsNullOrWhiteSpace($StartDir)) { return $null }
    if (-not (Test-Path -LiteralPath $StartDir)) { return $null }

    try {
        $current = (Resolve-Path -LiteralPath $StartDir).Path
    }
    catch {
        $current = $StartDir
    }

    for ($i = 0; $i -lt $MaxDepth; $i++) {
        $found = Test-DeploymentRootOrSubdir -Path $current
        if ($found) { return $found }

        $parent = Split-Path -Parent $current
        if ([string]::IsNullOrWhiteSpace($parent) -or $parent -eq $current) {
            break
        }
        $current = $parent
    }

    return $null
}
function Show-Status {
    param(
        [string]$DeploymentPath,
        [string]$ProtocolMode
    )

    Write-Step "Checking service status..."

    Invoke-Wsl -Command "cd '$DeploymentPath' && docker compose ps" -Root
    Reset-ConsoleLine

    Write-Block @(
        "Proto Fleet is now running!"
    )

    $protocol = if ($ProtocolMode -eq "https") { "https" } else { "http" }

    Write-Block @(
        "Access URLs:",
        "Local: ${protocol}://localhost"
    )

    $localIps = Invoke-WslRootCapture "hostname -I 2>/dev/null | tr ' ' '\n' | grep -v '^127\.' | head -n 3"
    if (-not [string]::IsNullOrWhiteSpace($localIps)) {
        foreach ($ip in ($localIps -split "`n")) {
            $ipTrimmed = $ip.Trim()
            if (-not [string]::IsNullOrWhiteSpace($ipTrimmed)) {
                Write-Host "LAN: ${protocol}://$ipTrimmed"
            }
        }
    }

    Write-Block @(
        "Useful commands:",
        "View logs: wsl bash -c `"cd $DeploymentPath && docker compose logs -f`"",
        "Stop: wsl bash -c `"cd $DeploymentPath && docker compose down`"",
        "Restart: wsl bash -c `"cd $DeploymentPath && docker compose restart`"",
        "Check status: wsl bash -c `"cd $DeploymentPath && docker compose ps`""
    )
}

# ============================================================================
# Main
# ============================================================================

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

if ($script:IsExe) {
    $logDir = Split-Path -Parent $exePath
    if ([string]::IsNullOrWhiteSpace($logDir)) {
        $logDir = (Get-Location).Path
    }
    $logPath = Join-Path $logDir "fleet-exe.log"
    try {
        Start-Transcript -Path $logPath -Append | Out-Null
        $script:TranscriptStarted = $true
        Write-Host "Logging to: $logPath"
    }
    catch {
        # Ignore transcript failures
    }

    $env:PROTOFLEET_EXE = "1"
    $env:PROTOFLEET_GUI_PROMPTS = "1"
}

if ($script:IsExe) {
    try {
        Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action {
            if (-not $global:ProtoFleetDidExitPause -and -not $env:PROTOFLEET_NO_PAUSE) {
                try {
                    [Console]::Write("`r")
                    Write-Host ""
                    [Console]::Write("`r")
                    [Console]::Write("Press Enter to exit")
                    if ($host -and $host.UI -and $host.UI.RawUI) {
                        $host.UI.RawUI.FlushInputBuffer()
                    }
                    [Console]::ReadLine() | Out-Null
                }
                catch {
                    Start-Sleep -Seconds 2
                }
            }
        } | Out-Null
    }
    catch {
        # Ignore event registration failures
    }
}

Write-Host ""
Write-Host "Proto Fleet - Windows Installer (WSL/Docker + Fleet)" -ForegroundColor Cyan
Write-Host ""

Initialize-GuiPrompts

if (-not $Silent) {
    $consentPrompt = @"
This installer will make changes to your Windows system:

- Enable and configure WSL2 (Windows Subsystem for Linux) so Linux tools can run on Windows
- Install the Ubuntu WSL distribution
- Install and configure Docker Engine inside WSL
- Install and start Proto Fleet services
- (Optional) Create a Windows scheduled task to auto-start WSL + Docker at login
  If you skip this, you’ll need to start WSL manually after reboot

Continue?
"@
    $consent = Read-YesNoPrompt $consentPrompt -DefaultYes:$true
    if (-not $consent) {
        Write-Host "Installer canceled by user."
        Invoke-Exit 0
    }
}

if ($Silent) {
    $Force = $true
    $script:SimpleSetup = $true
}
elseif ($Guided) {
    $script:SimpleSetup = $false
}
else {
    $setupOptions = @(
        @{ Value = "simple"; Label = "Simple (Recommended)"; Description = "Automatic configuration. Generates secure passwords and uses HTTP. Best for quick installs." },
        @{ Value = "guided"; Label = "Guided (Advanced)"; Description = "Step-by-step configuration including SSL options and custom credentials." }
    )
    $choice = Read-ChoicePrompt -Prompt "Choose setup type:" -Options $setupOptions -DefaultValue "simple"
    if ($choice -eq "guided") {
        $script:SimpleSetup = $false
    }
    else {
        $script:SimpleSetup = $true
    }
}

# WSL/Docker Setup
Write-Step "Checking WSL/Docker prerequisites..."

Start-Spinner -Activity "Checking WSL/Docker prerequisites"
$prereqWarnings = @()
$needsSetup = $false
try {
    $statusResult = Invoke-ProcessCapture -FileName "wsl.exe" -Arguments "--status" -TimeoutSeconds 60
    if ($statusResult.ExitCode -eq 0) {
        $script:CachedWslStatusText = $statusResult.Output
        $script:CachedWslStatusExitCode = $statusResult.ExitCode
    }
    else {
        $script:CachedWslStatusText = $null
        $script:CachedWslStatusExitCode = $statusResult.ExitCode
        if ($statusResult.Output) {
            $prereqWarnings += "WSL status check returned exit code $($statusResult.ExitCode)"
        }
    }

    if (Test-WslInstallNeededText -Text $statusResult.Output) {
        Stop-Spinner
        Ensure-WslPackageInstalled
        Start-Spinner -Activity "Checking WSL/Docker prerequisites"
    }

    if (-not (Test-WSLInstalled)) { $needsSetup = $true }
    if (-not (Test-DockerInWSL)) { $needsSetup = $true }
}
finally {
    Stop-Spinner
}

if ($prereqWarnings.Count -gt 0) {
    foreach ($w in $prereqWarnings) {
        Write-WarningMsg $w
    }
}

Ensure-WslUpdated | Out-Null

if ($needsSetup) {
    if (-not (Test-Administrator)) {
        Write-ErrorMsg "Setup requires Administrator privileges."
        Write-Host ""
        Write-Host "Please re-run this installer in an elevated PowerShell."
        Invoke-Exit 1
    }

    Test-SystemRequirements
    Enable-WSLFeature
    Set-WSL2AsDefault
    Install-WSLDistribution
    Select-WslDistro
    Ensure-WslAutomountReady | Out-Null
    Ensure-SystemdEnabled
    Install-DockerInWSL
    Set-WSLNetworkingFixes

    if (-not (Test-DockerInstallation)) {
        Write-ErrorMsg "Docker verification failed."
        Invoke-Exit 1
    }

    Ensure-WslAutoStartTask
    Write-Success "WSL/Docker setup complete"
}
else {
    Write-Success "WSL and Docker are already installed and running"
    Select-WslDistro
    Ensure-WslAutomountReady | Out-Null
    Ensure-DockerUserAccess
    Ensure-WslAutoStartTask
}

# Fleet Installation
Write-Step "Running Proto Fleet installation..."

$scriptBaseDir = if ($MyInvocation.MyCommand.Path) {
    Split-Path -Parent $MyInvocation.MyCommand.Path
}
elseif ($PSScriptRoot) {
    $PSScriptRoot
}
else {
    try {
        Split-Path -Parent ([System.Diagnostics.Process]::GetCurrentProcess().MainModule.FileName)
    }
    catch {
        (Get-Location).Path
    }
}

# $script:SimpleSetup set from setup selection above

$skipExtraction = $false

$explicitDeployment = if (-not [string]::IsNullOrWhiteSpace($DeploymentPath)) {
    $DeploymentPath
}
elseif (-not [string]::IsNullOrWhiteSpace($env:PROTOFLEET_DEPLOYMENT_PATH)) {
    $env:PROTOFLEET_DEPLOYMENT_PATH
}
else {
    ""
}

if (-not [string]::IsNullOrWhiteSpace($explicitDeployment)) {
    Write-Step "Using provided deployment path: $explicitDeployment"
    $foundDeployment = Find-DeploymentRoot -StartDir $explicitDeployment -MaxDepth 12
    if (-not $foundDeployment) {
        Write-ErrorMsg "Deployment files not found under: $explicitDeployment"
        Write-Host "Expected docker-compose.yaml plus server/ and client/ directories."
        Invoke-Exit 3
    }
    Write-Success "Using deployment files from: $foundDeployment"
    $deploymentPath = $foundDeployment
    $skipExtraction = $true
}
if (-not $skipExtraction -and -not [string]::IsNullOrWhiteSpace($TarballPath)) {
    Write-Step "Using local tarball: $TarballPath"

    if (-not (Test-Path $TarballPath)) {
        Write-ErrorMsg "Tarball file not found: $TarballPath"
        Invoke-Exit 3
    }

    $tarName = Split-Path -Leaf $TarballPath
    if ($tarName -notmatch '^proto-fleet-.*\.tar\.gz$') {
        Write-ErrorMsg "Invalid tarball name. Expected format: proto-fleet-*.tar.gz"
        Invoke-Exit 3
    }

    $wslTempPath = "/tmp/$tarName"
    Copy-ToWSL -WindowsFilePath $TarballPath -WSLTempPath $wslTempPath
    Write-Success "Local tarball copied to WSL"
}
elseif (-not $skipExtraction -and ($detectedDeployment = Find-DeploymentRoot -StartDir $scriptBaseDir)) {
    Write-Host ""
    Write-Host "Detected Proto Fleet deployment files in: $detectedDeployment"
    Write-Success "Using deployment files from: $detectedDeployment"
    $deploymentPath = $detectedDeployment
    $skipExtraction = $true
}
elseif (-not $skipExtraction -and ($detectedDeployment = Find-DeploymentRoot -StartDir (Get-Location).Path)) {
    Write-Host ""
    Write-Host "Detected Proto Fleet deployment files in: $detectedDeployment"
    Write-Success "Using deployment files from: $detectedDeployment"
    $deploymentPath = $detectedDeployment
    $skipExtraction = $true
}
else {
    Write-Host ""
    Write-Host "No deployment files detected in current directory."
    Write-Host ""
    Write-Host "Please provide the path to your proto-fleet-*.tar.gz tarball."
    Write-Host ""

    if (-not $Force -and -not $Silent) {
        if ($script:UseGuiPrompts) {
            $TarballPath = Show-OpenFileDialog -Title "Select Proto Fleet tarball" -Filter "Proto Fleet tarball (*.tar.gz)|*.tar.gz|All files (*.*)|*.*"
        }
        else {
            $TarballPath = Read-HostLine "Enter tarball path"
        }
    }

    if ([string]::IsNullOrWhiteSpace($TarballPath) -or -not (Test-Path $TarballPath)) {
        Write-ErrorMsg "Invalid or missing tarball path"
        Invoke-Exit 3
    }

    $tarName = Split-Path -Leaf $TarballPath
    if ($tarName -notmatch '^proto-fleet-.*\.tar\.gz$') {
        Write-ErrorMsg "Invalid tarball name. Expected format: proto-fleet-*.tar.gz"
        Invoke-Exit 3
    }

    $wslTempPath = "/tmp/$tarName"
    Copy-ToWSL -WindowsFilePath $TarballPath -WSLTempPath $wslTempPath
    Write-Success "Local tarball copied to WSL"
}

if ($skipExtraction) {
    $deploymentWindowsPath = $deploymentPath
    $deploymentPath = ConvertTo-WSLPath -WindowsPath $deploymentPath
    if ($env:PROTOFLEET_DEBUG -eq "1") {
        Write-Host "DEBUG: Deployment Windows path: $deploymentWindowsPath"
        Write-Host "DEBUG: Deployment WSL path: $deploymentPath"
    }

    if ($deploymentPath -match "'") {
        Write-WarningMsg "Deployment path contains an apostrophe, which breaks shell commands in WSL."
        Write-Host "Attempting to copy the deployment into WSL to avoid quoting issues..."
        $deploymentPath = Copy-DeploymentIntoWSL -WindowsDeploymentPath $deploymentWindowsPath -TargetDir "~/proto-fleet"
    }
    else {
    $deploymentVisible = Test-WslPathExists -Path $deploymentPath -Directory -Root
    if ($env:PROTOFLEET_DEBUG -eq "1") {
        Write-Host "DEBUG: Test-WslPathExists dir $deploymentPath => $deploymentVisible"
    }

    if (-not $deploymentVisible) {
        $mountOk = Test-WslMountAvailable
        if ($mountOk) {
            Write-WarningMsg "WSL automount is available, but the deployment path is not visible inside WSL."
            Write-Host "Reason: path conversion mismatch or mount mapping differs from expected."
        }
        else {
            Write-WarningMsg "WSL automount is not available; deployment path is not visible inside WSL."
        }
        Write-Host "Attempting to copy the deployment into WSL..."
        $deploymentPath = Copy-DeploymentIntoWSL -WindowsDeploymentPath $deploymentWindowsPath -TargetDir "~/proto-fleet"
    }
    }
}
else {
    $finalInstallDir = Set-InstallDirectory -DefaultDir $InstallDir
    $deploymentPath = Expand-InWSL -TarPath $wslTempPath -TargetDir $finalInstallDir
    if ($env:PROTOFLEET_DEBUG -eq "1") {
        Write-Host "DEBUG: Deployment WSL path: $deploymentPath"
    }
}

Test-PluginBinaries -DeploymentPath $deploymentPath

if (-not [string]::IsNullOrWhiteSpace($ConfigFile)) {
    Write-Step "Using provided configuration file: $ConfigFile"

    if (-not (Test-Path $ConfigFile)) {
        Write-ErrorMsg "Config file not found: $ConfigFile"
        Invoke-Exit 1
    }

    $wslConfigPath = ConvertTo-WSLPath -WindowsPath $ConfigFile
    $targetEnvFile = "$deploymentPath/.env"

    Invoke-WslExec -Executable "/bin/cp" -Arguments @("--", $wslConfigPath, $targetEnvFile) -Root | Out-Null
    Invoke-WslExec -Executable "/bin/chmod" -Arguments @("600", $targetEnvFile) -Root | Out-Null
    Ensure-EnvFileOwnership -EnvFilePath $targetEnvFile

    if (-not (Test-EnvFileComplete -EnvFilePath $targetEnvFile)) {
        Write-ErrorMsg "Provided config file is missing required keys"
        Invoke-Exit 1
    }

    Write-Success "Configuration file copied"
}
else {
    New-EnvironmentFile -DeploymentPath $deploymentPath | Out-Null
}

$protocolMode = Set-SSLConfiguration -DeploymentPath $deploymentPath

Start-DockerCompose -DeploymentPath $deploymentPath

Wait-ForHealthyServices -DeploymentPath $deploymentPath | Out-Null

Show-Status -DeploymentPath $deploymentPath -ProtocolMode $protocolMode

Write-Success "Full installation complete."
Write-WarningMsg "If you cannot reach Fleet yet, open PowerShell and run: wsl"
Invoke-Exit 0



