# WSL Background/Keepalive Trials

Last updated: 2026-02-11
Environment: Windows 11 Enterprise 26200, WSL 2.6.3.0, Ubuntu (Default)

**Tried**
1. Option 2 (interactive background task via shell): `nohup sleep <seconds> &` started from Windows via `wsl.exe` (keepalive helper).
Result: Previous keepalive PID verification was unreliable. Updated to PID-file based keepalive on 2026-02-11. Re-test pending with new keepalive implementation.
2. Systemd-managed Docker services: `systemctl start docker` and Docker Compose.
Result: Docker and containers run and remain healthy during active probes; WSL stays running while probes continue. When probes are disabled, WSL has previously stopped after ~20 seconds even with Docker running (see logs).

**Not tried yet**
1. Option 1 (interactive service via `service` or direct daemon start) with systemd disabled.
Notes: The AskUbuntu post indicates Option 1 can keep WSL alive only when systemd is disabled. We have not disabled systemd in `/etc/wsl.conf` yet.
2. AskUbuntu final solution: start an interactive keepalive from shell startup (e.g., `keychain` starting `ssh-agent`) so WSL stays alive after terminal exit; stop by killing the agent (`keychain -k all`).
3. Alternate answer: Windows login task/script to launch `wsl -d <Distro>` hidden on login, plus optional `dbus-daemon` keepalive from `~/.bashrc`.

**Next tests**
1. Re-run with keepalive enabled (updated PID-file method) and passive monitor to see if WSL remains running without probes.
2. If still shutting down, test Option 1 by temporarily disabling systemd and starting a simple service like `cron` via `service` or direct daemon, then exit the shell and observe WSL state.
