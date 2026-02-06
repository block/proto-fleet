# Proto Fleet Windows Installer

This folder contains the Windows installer script and the PS2EXE builder used to generate a standalone EXE.

## Files
- `fleet-installer.ps1`: The installer script (runs in PowerShell or when compiled to EXE).
- `build-fleet-installer-exe.ps1`: Builds `fleet.exe` using PS2EXE.
- `fleet.exe`: The compiled Windows installer (generated in CI and included in release tarballs).

## Build the EXE
From this directory:

```powershell
./build-fleet-installer-exe.ps1
```

Defaults:
- Input script: `./fleet-installer.ps1`
- Output EXE: `./fleet.exe`

You can override both:

```powershell
./build-fleet-installer-exe.ps1 -InputScript .\fleet-installer.ps1 -OutputExe .\fleet.exe
```

## Run the installer
You can run the script directly:

```powershell
./fleet-installer.ps1
```

Or run the EXE:

```powershell
./fleet.exe
```

## Default behavior (double‑clicking the EXE)
If you build `fleet.exe` with no custom parameters and simply run it (double‑click), the installer uses these defaults:

- `InstallDir`: `~/proto-fleet` inside WSL (resolved to the WSL user’s home)
- `Guided`: off (no extra guided prompts)
- `Simple setup`: on
- `SSL mode`: HTTP (no TLS)
- Credentials: auto‑generated for backend services
- Deployment discovery: searches upward from the EXE location and current working directory
- If no deployment is found: prompts for a local `proto-fleet-*.tar.gz` tarball
- WSL/Docker: installs and configures if missing; may prompt to enable auto‑start at login

## Release packaging
Release CI includes `fleet.exe` inside the deployment bundle at `deployment/install/fleet.exe`.

## How it finds the deployment
The installer looks for an extracted Proto Fleet release (deployment root) by searching upward from:
- The EXE/script location
- The current working directory

A valid deployment root must contain:
- `docker-compose.yaml`
- `server/`
- `client/`

It also accepts an explicit deployment path:

```powershell
./fleet-installer.ps1 -DeploymentPath "C:\path\to\extracted\release\any\subfolder"
```

Or via environment variable:

```powershell
$env:PROTOFLEET_DEPLOYMENT_PATH = "C:\path\to\extracted\release"
./fleet.exe
```

If no extracted deployment is found, the installer will prompt for a `proto-fleet-*.tar.gz` tarball.

## Common options
- `-TarballPath`: Path to a local `proto-fleet-*.tar.gz`
- `-ConfigFile`: Path to a `.env` config file
- `-Guided`: Enable guided prompts
- `-Silent`: Non-interactive mode
- `-Force`: Skip confirmation prompts where possible

## Notes
- The installer requires Administrator privileges for WSL/Docker setup.
- If the deployment is under a Windows path that WSL cannot access, it will copy the deployment into WSL automatically.
