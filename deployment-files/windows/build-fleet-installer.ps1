param(
    [ValidateSet("Debug", "Release")]
    [string]$Configuration = "Release",
    [string]$Runtime = "win-x64",
    [string]$OutputDir = "$PSScriptRoot\artifacts\release-installer"
)

$ErrorActionPreference = "Stop"

$project = Join-Path $PSScriptRoot "src\ProtoFleet.Installer.App\ProtoFleet.Installer.App.csproj"
$nugetConfig = Join-Path $PSScriptRoot "NuGet.Config"

$dotnetCommand = Get-Command dotnet -ErrorAction SilentlyContinue
$dotnet = $null
if ($dotnetCommand) {
    $dotnet = $dotnetCommand.Source
}

if (-not $dotnet) {
    $defaultDotnet = Join-Path ${env:ProgramFiles} "dotnet\dotnet.exe"
    if (Test-Path $defaultDotnet) {
        $dotnet = $defaultDotnet
    }
}

if (-not $dotnet) {
    throw "dotnet SDK is required to build the C# installer."
}

if (-not (Test-Path $project)) {
    throw "Installer project was not found at $project."
}

if (-not (Test-Path $nugetConfig)) {
    throw "NuGet.Config was not found at $nugetConfig."
}

if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    throw "OutputDir must be a directory path."
}

if ($OutputDir.ToLowerInvariant().EndsWith(".exe")) {
    throw "OutputDir must be a directory path, not an .exe file path."
}

if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

& $dotnet publish $project `
    -c $Configuration `
    -r $Runtime `
    --configfile $nugetConfig `
    --self-contained true `
    /p:PublishSingleFile=true `
    /p:IncludeNativeLibrariesForSelfExtract=true `
    /p:DebugType=None `
    /p:DebugSymbols=false `
    /p:CopyOutputSymbolsToPublishDirectory=false `
    -o $OutputDir

if ($LASTEXITCODE -ne 0) {
    throw "dotnet publish failed with exit code $LASTEXITCODE."
}

$installerPath = Join-Path $OutputDir "installer.exe"
if (Test-Path $installerPath) {
    Remove-Item -LiteralPath $installerPath -Force
}

$exeFiles = @(Get-ChildItem -Path $OutputDir -Filter "*.exe" -File -ErrorAction SilentlyContinue)
$candidate = $exeFiles |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1
if (-not $candidate) {
    throw "Publish completed but no installer .exe was found in $OutputDir."
}

if ($candidate.FullName -ne $installerPath) {
    Move-Item -LiteralPath $candidate.FullName -Destination $installerPath -Force
}

$extraExeFiles = @(Get-ChildItem -Path $OutputDir -Filter "*.exe" -File -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -ne $installerPath })

foreach ($extraExe in $extraExeFiles) {
    Remove-Item -LiteralPath $extraExe.FullName -Force
}

if (-not (Test-Path $installerPath)) {
    throw "Publish completed but installer output was not created at $installerPath."
}

Write-Host "Installer published to $installerPath"
