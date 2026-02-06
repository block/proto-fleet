param(
    [string]$InputScript = ".\\fleet-installer.ps1",
    [string]$OutputExe = ".\\fleet.exe"
)

$ErrorActionPreference = "Stop"

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
        Write-Error "Failed to remove existing $OutputExe. Close any running fleet.exe and try again."
        exit 1
    }
}

Invoke-ps2exe -InputFile $InputScript -OutputFile $OutputExe -RequireAdmin -STA

if (Test-Path $OutputExe) {
    Write-Host "Built $OutputExe"
}
else {
    Write-Error "Failed to build $OutputExe."
    exit 1
}



