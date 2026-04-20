# Proto Fleet Windows Installer

This directory contains the C# WPF Windows installer implementation:

- `src/ProtoFleet.Installer.App`

## Installer (v1)

### Highlights
- Single self-contained `win-x64` `.exe`
- Native WPF wizard UI
- Elevation at launch (UAC)
- Native C# orchestration for WSL + Docker + deployment flow
- Minimal CLI inputs with GUI-first behavior

### Build
From this directory:

```powershell
./build-fleet-installer.ps1
```

From the repository root:

```powershell
./deployment-files/windows/build-fleet-installer.ps1
```

`build-fleet-installer.ps1` uses the local `NuGet.Config` in this folder, so it does not depend on user-level NuGet source setup.
The local `global.json` pins the .NET SDK line to .NET 8 for reproducible builds even if newer SDKs (for example .NET 10) are installed.

The generated executable is `installer.exe`.

## Uninstaller

Build from this directory:

```powershell
Install-Module -Name ps2exe -Scope CurrentUser -Force
Get-Command Invoke-ps2exe
```

Then build:

```powershell
./build-fleet-uninstaller-exe.ps1
```

The generated executable is `uninstall.exe`.

Uninstaller distro behavior:
- If `-WslDistro` is provided, that distro is used.
- If `-WslDistro` is omitted, the uninstaller auto-selects an installed distro:
  - default distro first
  - then first `Ubuntu*` distro
  - then first available distro

Uninstaller deployment path detection order:
1. `-DeploymentPath` (if provided)
2. `-InstallDir` (if provided)
3. default `~/proto-fleet/deployment`
4. default `~/proto-fleet`
5. prompt for one explicit path (or fail in `-Silent` mode)

If you are prompted unexpectedly, verify what the uninstaller resolves in your selected distro:

```powershell
wsl -d <YourDistro> -- bash -lc 'printf "HOME=%s\n" "$HOME"; ls -la ~/proto-fleet/deployment'
```

Common failure signatures:
- `The term 'id' is not recognized...`
  - Cause: host-side PowerShell interpolation of a WSL command (old/unpatched uninstaller build).
  - Fix: rebuild and rerun `uninstall.exe` from current sources.
- `Using WSL distro: U b u n t u`
  - Cause: stale uninstaller output parsing from older build.
  - Fix: rebuild and rerun `uninstall.exe` from current sources.

Uninstaller execution model:
- Runs in user context (no `-RequireAdmin` in the uninstaller EXE build).
- Executes WSL commands as the distro default user (no `-u root`).
- Uses `~/proto-fleet` as the primary install root and `~/proto-fleet/deployment` as deployment root.
- Prompts for path only if deployment is not found at the default location.
- Rejects paths outside `/home/<user>/proto-fleet` or `/home/<user>/proto-fleet/deployment` for safety.

Uninstall order:
1. Tear down containers.
2. Delete images.
3. Remove Proto Fleet systemd user artifacts.
4. Delete `~/proto-fleet` from the WSL user home.

## Release Bundle Contents

Release tarball `proto-fleet-<tag>.tar.gz` includes both Windows executables at:
- `deployment/install/installer.exe`
- `deployment/install/uninstall.exe`

## Prerequisites (Windows 11)

Run all setup commands in an elevated PowerShell session.

### Required
- Windows 11 (x64) with local Administrator access
- Virtualization enabled in BIOS/UEFI
- .NET SDK 8.x (compatible with `global.json`)
- WSL feature support (`Microsoft-Windows-Subsystem-Linux`, `VirtualMachinePlatform`)

### Install with winget (preferred)
```powershell
winget --version
winget install --id Microsoft.DotNet.SDK.8 --source winget
winget install --id Microsoft.WSL --source winget
```

After installing prerequisites, open a new elevated PowerShell session before continuing so updated PATH/tooling is available.

### Fallback official links
- .NET SDK 8 downloads: `https://dotnet.microsoft.com/download/dotnet/8.0`
- WSL install docs: `https://learn.microsoft.com/windows/wsl/install`

### Quick verification
```powershell
dotnet --info
wsl --status
```

### Optional CLI Inputs
- `-DeploymentPath <path>`
- `-TarballPath <path>`
- `-ConfigFile <path>`
- `-InstallDir <wsl-path>`
- `-Version <label>`
- `-Guided true|false`
- `-ProtocolMode auto|http|https-self-signed|https-existing`
- `-EnableAutoStartTask true|false`

### Project Layout
- `ProtoFleet.Installer.sln`
- `src/ProtoFleet.Installer.App`: WPF UI and orchestration wiring
- `src/ProtoFleet.Installer.Core`: workflow contracts, shared services, step runner
- `src/ProtoFleet.Installer.Platform.Windows`: host checks, elevation, scheduled task
- `src/ProtoFleet.Installer.Platform.Wsl`: WSL/Docker/deployment operations
- `tests/ProtoFleet.Installer.Tests`: unit tests for parser/resolution/env logic

### WSL Ubuntu Install Notes
- The installer intentionally tries multiple WSL install command forms for Ubuntu:
  - `wsl --install --no-launch -d <name>`
  - `wsl --install --no-launch --distribution <name>`
  - `wsl --install --no-launch <name>`
- Reason: WSL behavior can vary by Windows/WSL build, and some environments accept one form while rejecting another.
- If web-download is needed, the installer prefers concrete distro names (for example `Ubuntu-24.04`, `Ubuntu-22.04`) because the generic `Ubuntu` alias can intermittently return HTTP 404 from the web catalog.
- Linux user provisioning behavior:
  - Linux user provisioning is interactive-only: complete Ubuntu first-run username/password setup in the Ubuntu window.
  - Installer shows recovery actions (`Check Setup`, `Open Ubuntu Window`, `Copy Command`) while monitoring for completion.
- If Windows reboot is required during WSL setup, installer stores resume state and registers a one-time auto-resume entry so setup continues automatically after reboot.

## Manual Test Plan (Windows 11)

### Safety warning
The reset steps below are intentionally destructive.
- `wsl --unregister <Distro>` permanently deletes that distro filesystem.
- Disabling Windows features requires reboot and may impact other local workflows.

Back up any required WSL data before running these tests.

### 1) Baseline environment capture
```powershell
dotnet --info
wsl --status
wsl --list --verbose
Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux |
  Select-Object FeatureName,State
Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform |
  Select-Object FeatureName,State
```
Expected:
- `dotnet` resolves and reports SDK compatible with `global.json`.
- You can see current WSL state and installed distros.

### 2) Scenario A: Fresh machine path (no Ubuntu distro)
```powershell
wsl --shutdown
wsl --list --verbose
# If Ubuntu exists, unregister it:
wsl --unregister Ubuntu
wsl --unregister Ubuntu-22.04
wsl --unregister Ubuntu-24.04
```
Then run installer and validate:
- Installer handles distro installation path.
- Installer reaches Linux user setup wait step and resumes correctly after setup.

### 3) Scenario B: Force WSL default version 1
```powershell
wsl --set-default-version 1
wsl --status
```
Then run installer and validate:
- Installer detects/fixes prerequisites for modern WSL flow.
- Final deployment succeeds.

### 4) Scenario C: Disable WSL features entirely
```powershell
wsl --shutdown
dism.exe /online /disable-feature /featurename:Microsoft-Windows-Subsystem-Linux /norestart
dism.exe /online /disable-feature /featurename:VirtualMachinePlatform /norestart
shutdown /r /t 0
```
After reboot:
```powershell
Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux |
  Select-Object FeatureName,State
Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform |
  Select-Object FeatureName,State
```
Then run installer and validate:
- Installer requests/enables required feature setup path.
- Reboot-required flow is handled.

### 5) Scenario D: Reboot/resume behavior
Trigger any path that returns reboot required, then verify:
- Auto-resume state is persisted.
- Installer resumes after reboot and continues from checkpoint.
- End state reaches completion page.

### 6) Scenario E: Packaging verification
```powershell
.\build-fleet-installer.ps1 -Configuration Release -OutputDir .\artifacts\release-installer
.\build-fleet-uninstaller-exe.ps1 -OutputExe .\artifacts\release-installer\uninstall.exe
Get-ChildItem .\artifacts\release-installer\installer.exe
Get-ChildItem .\artifacts\release-installer\uninstall.exe
```
Expected:
- `installer.exe` exists at the specified output directory.
- `uninstall.exe` exists at the specified output directory.

### 7) Restore machine defaults after testing
```powershell
dism.exe /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart
dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart
wsl --set-default-version 2
shutdown /r /t 0
```

After reboot, optional Ubuntu reinstall:
```powershell
wsl --install -d Ubuntu
```
