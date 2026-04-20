param(
    [string]$InputScript = ".\\fleet-uninstaller.ps1",
    [string]$OutputExe = ".\\uninstall.exe"
)

$ErrorActionPreference = "Stop"

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not [System.IO.Path]::IsPathRooted($InputScript)) {
    $InputScript = Join-Path $scriptRoot $InputScript
}
if (-not [System.IO.Path]::IsPathRooted($OutputExe)) {
    $OutputExe = Join-Path $scriptRoot $OutputExe
}

$outputDir = Split-Path -Parent $OutputExe
if (-not [string]::IsNullOrWhiteSpace($outputDir) -and -not (Test-Path $outputDir)) {
    New-Item -ItemType Directory -Path $outputDir | Out-Null
}

Import-Module ps2exe -ErrorAction Stop

if (-not (Test-Path $InputScript)) {
    Write-Error "Input script not found: $InputScript"
    exit 1
}

if (Test-Path $OutputExe) {
    try {
        Remove-Item -LiteralPath $OutputExe -Force
    }
    catch {
        Write-Error "Failed to remove existing $OutputExe. Close any running uninstall.exe and try again."
        exit 1
    }
}

Invoke-ps2exe -InputFile $InputScript -OutputFile $OutputExe -STA

if (Test-Path $OutputExe) {
    Write-Host "Built $OutputExe"
}
else {
    Write-Error "Failed to build $OutputExe."
    exit 1
}
