using ProtoFleet.Installer.Core;
using System.Text.RegularExpressions;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class DockerReadinessService : IDockerReadinessService
{
    private const int DockerInstallAttempts = 5;
    private const int DockerVerifyAttempts = 8;
    private const int DockerDaemonWaitChecks = 20;
    private readonly WslCommandExecutor _executor;
    private readonly ILogSink _logSink;
    private readonly WslRecoveryService _recoveryService;

    public DockerReadinessService(WslCommandExecutor executor, ILogSink logSink)
    {
        _executor = executor;
        _logSink = logSink;
        _recoveryService = new WslRecoveryService(executor, logSink);
    }

    public async Task<InstallerStepResult> EnsureReadyAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(context.SelectedDistro))
        {
            return InstallerStepResult.Failed("WSL distro must be selected before Docker setup.");
        }

        var distro = context.SelectedDistro!;
        _logSink.Info($"Verifying Docker readiness in distro '{distro}'.");

        var ready = await CheckDockerReadyAsync(distro, cancellationToken);
        if (ready.IsSuccess)
        {
            return InstallerStepResult.Succeeded();
        }

        _logSink.Warn($"Docker is not ready in '{distro}'. Attempting installation and remediation.");
        var installDnsRemediationApplied = false;
        var install = await RetryPolicy.ExecuteAsync(
            attempts: DockerInstallAttempts,
            action: async attempt =>
            {
                _logSink.Info($"Docker install attempt {attempt}/{DockerInstallAttempts}");
                var result = await InstallDockerEngineAsync(distro, cancellationToken);
                if (!result.IsSuccess)
                {
                    var installTail = await ReadInstallLogTailAsync(distro, cancellationToken);
                    var installOutput = CombineInstallOutput(result, installTail);
                    if (WslOutputClassifier.LooksDnsIssue(installOutput))
                    {
                        _logSink.Warn(
                            "Detected DNS resolver failure during Docker install. " +
                            "Applying WSL DNS remediation and restarting WSL before retry.");
                        AddWarningOnce(context, "Applied WSL DNS fix during Docker installation retries.");
                        await _recoveryService.ApplyDnsFixAsync(distro, cancellationToken);
                        await _recoveryService.ResetWslAsync(cancellationToken);
                        installDnsRemediationApplied = true;
                    }
                    else if (WslOutputClassifier.LooksAptRepositoryReachabilityIssue(installOutput))
                    {
                        _logSink.Warn(
                            "Docker install failed due to Ubuntu repository reachability. " +
                            $"install-log={Clip(installTail.StandardOutput)}");
                    }
                }

                return result;
            },
            isSuccess: result => result.IsSuccess,
            backoff: attempt => TimeSpan.FromSeconds(Math.Min(30, Math.Pow(2, attempt))));

        var dockerCliPresent = await IsDockerCliPresentAsync(distro, cancellationToken);
        if (!install.IsSuccess && !dockerCliPresent)
        {
            var installTail = await ReadInstallLogTailAsync(distro, cancellationToken);
            var installOutput = CombineInstallOutput(install, installTail);
            if (WslOutputClassifier.LooksDnsIssue(installOutput) ||
                WslOutputClassifier.LooksAptRepositoryReachabilityIssue(installOutput))
            {
                return InstallerStepResult.Failed(
                    "Docker installation inside WSL failed after retries because Ubuntu package repositories " +
                    "were unreachable from WSL (DNS/network). " +
                    $"{CommandDetails(install)} install-log={Clip(installTail.StandardOutput)}");
            }

            return InstallerStepResult.Failed(
                "Docker installation inside WSL failed after retries. " +
                $"{CommandDetails(install)} install-log={Clip(installTail.StandardOutput)}");
        }

        if (!install.IsSuccess && dockerCliPresent)
        {
            _logSink.Warn("Docker install command returned non-zero, but Docker CLI is present. Continuing.");
            AddWarningOnce(context, "Docker installer returned a non-zero exit code, but Docker CLI is available.");
        }
        else if (installDnsRemediationApplied)
        {
            _logSink.Info("Docker installation succeeded after WSL DNS remediation.");
        }

        var start = await EnableAndStartDockerAsync(distro, cancellationToken);
        if (!start.IsSuccess)
        {
            _logSink.Warn($"Initial Docker start command returned non-zero. {CommandDetails(start)}");
        }

        var daemonReady = await WaitForDockerDaemonAsync(distro, cancellationToken);
        if (!daemonReady.IsSuccess)
        {
            var diagnostics = await CollectDockerDiagnosticsAsync(distro, cancellationToken);
            return InstallerStepResult.Failed(
                "Docker daemon failed to start in WSL after retries. " +
                $"{CommandDetails(daemonReady)} diagnostics={Clip(diagnostics)}");
        }

        await EnsureDockerUserAccessAsync(distro, cancellationToken);
        await ApplyBaselineNetworkingFixesAsync(distro, cancellationToken, context);

        var verify = await RetryPolicy.ExecuteAsync(
            attempts: DockerVerifyAttempts,
            action: async attempt => await VerifyDockerWithRemediationAsync(distro, context, attempt, cancellationToken),
            isSuccess: result => result.IsSuccess,
            backoff: attempt => TimeSpan.FromSeconds(Math.Min(20, attempt * 3)));

        if (!verify.IsSuccess)
        {
            var diagnostics = await CollectDockerDiagnosticsAsync(distro, cancellationToken);
            return InstallerStepResult.Failed(
                "Docker did not become healthy inside WSL. " +
                $"{CommandDetails(verify)} diagnostics={Clip(diagnostics)}");
        }

        _logSink.Info("Docker readiness confirmed in WSL.");
        return InstallerStepResult.Succeeded();
    }

    private Task<CommandResult> CheckDockerReadyAsync(string distro, CancellationToken cancellationToken)
    {
        return _executor.RunInDistroAsync(
            distro,
            "command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && docker compose version >/dev/null 2>&1",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(45));
    }

    private Task<CommandResult> InstallDockerEngineAsync(string distro, CancellationToken cancellationToken)
    {
        const string script =
            "set -euo pipefail; " +
            "export DEBIAN_FRONTEND=noninteractive CI=1 TERM=dumb LC_ALL=C; " +
            "LOG_FILE=/tmp/pf-docker-install.log; " +
            "rm -f \"$LOG_FILE\"; " +
            "{ " +
            "rm -rf /var/lib/apt/lists/* >/dev/null 2>&1 || true; " +
            "apt-get clean >/dev/null 2>&1 || true; " +
            "apt-get update; " +
            "curl -fsSL https://get.docker.com -o /tmp/get-docker.sh; " +
            "chmod +x /tmp/get-docker.sh; " +
            "/bin/sh /tmp/get-docker.sh; " +
            "} >\"$LOG_FILE\" 2>&1 || { tail -n 200 \"$LOG_FILE\" 2>/dev/null || true; exit 1; }; " +
            "tail -n 40 \"$LOG_FILE\" 2>/dev/null || true";

        return _executor.RunInDistroAsync(
            distro,
            script,
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromMinutes(15));
    }

    private Task<CommandResult> EnableAndStartDockerAsync(string distro, CancellationToken cancellationToken)
    {
        return _executor.RunInDistroAsync(
            distro,
            "systemctl enable docker 2>/dev/null || true; " +
            "service docker start 2>/dev/null || systemctl start docker 2>/dev/null || /etc/init.d/docker start 2>/dev/null || true",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(60));
    }

    private async Task<CommandResult> WaitForDockerDaemonAsync(string distro, CancellationToken cancellationToken)
    {
        CommandResult? last = null;
        for (var attempt = 1; attempt <= DockerDaemonWaitChecks; attempt++)
        {
            var probe = await _executor.RunInDistroAsync(
                distro,
                "systemctl is-active docker >/dev/null 2>&1; " +
                "if [ -S /run/docker.sock ] || [ -S /var/run/docker.sock ]; then exit 0; else exit 1; fi",
                asRoot: true,
                cancellationToken,
                timeout: TimeSpan.FromSeconds(20));

            if (probe.IsSuccess)
            {
                _logSink.Info("Docker daemon is running.");
                return probe;
            }

            last = probe;
            if (attempt % 5 == 0)
            {
                _logSink.Warn($"Docker daemon not ready yet (attempt {attempt}/{DockerDaemonWaitChecks}). Restarting docker service.");
                await RestartDockerServiceAsync(distro, cancellationToken);
            }

            await Task.Delay(TimeSpan.FromSeconds(2), cancellationToken);
        }

        return last ?? new CommandResult
        {
            ExitCode = 1,
            StandardError = "Docker daemon readiness probe did not execute."
        };
    }

    private Task<CommandResult> RestartDockerServiceAsync(string distro, CancellationToken cancellationToken)
    {
        return _executor.RunInDistroAsync(
            distro,
            "systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || /etc/init.d/docker restart 2>/dev/null || true",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(40));
    }

    private async Task EnsureDockerUserAccessAsync(string distro, CancellationToken cancellationToken)
    {
        var access = await _executor.RunInDistroAsync(
            distro,
            "set -e; " +
            "groupadd -f docker; " +
            "USER_NAME=$(getent passwd 1000 | cut -d: -f1); " +
            "if [ -n \"$USER_NAME\" ]; then usermod -aG docker \"$USER_NAME\"; fi; " +
            "if [ -S /var/run/docker.sock ]; then chgrp docker /var/run/docker.sock || true; chmod 660 /var/run/docker.sock || true; fi",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(30));

        if (!access.IsSuccess)
        {
            _logSink.Warn($"Could not fully configure docker group access. {CommandDetails(access)}");
        }
    }

    private async Task ApplyBaselineNetworkingFixesAsync(
        string distro,
        CancellationToken cancellationToken,
        InstallerContext context)
    {
        var fix = await _executor.RunInDistroAsync(
            distro,
            "set -e; " +
            "if ! grep -qF 'precedence ::ffff:0:0/96 100' /etc/gai.conf 2>/dev/null; then echo 'precedence ::ffff:0:0/96 100' >> /etc/gai.conf; fi; " +
            "sysctl -w net.ipv6.conf.all.disable_ipv6=1 >/dev/null 2>&1 || true; " +
            "sysctl -w net.ipv6.conf.default.disable_ipv6=1 >/dev/null 2>&1 || true; " +
            "if ! grep -q '^net.ipv6.conf.all.disable_ipv6=1' /etc/sysctl.conf 2>/dev/null; then echo 'net.ipv6.conf.all.disable_ipv6=1' >> /etc/sysctl.conf; fi; " +
            "if ! grep -q '^net.ipv6.conf.default.disable_ipv6=1' /etc/sysctl.conf 2>/dev/null; then echo 'net.ipv6.conf.default.disable_ipv6=1' >> /etc/sysctl.conf; fi; " +
            "if [ -f /etc/resolv.conf ] && ! grep -q 'nameserver 8.8.8.8' /etc/resolv.conf 2>/dev/null; then cp /etc/resolv.conf /etc/resolv.conf.backup.$(date +%s) 2>/dev/null || true; echo 'nameserver 8.8.8.8' >> /etc/resolv.conf; fi; " +
            "if [ -f /etc/wsl.conf ]; then " +
            "  if grep -q 'generateResolvConf *= *false' /etc/wsl.conf 2>/dev/null; then :; " +
            "  elif grep -q 'generateResolvConf' /etc/wsl.conf 2>/dev/null; then sed -i 's/generateResolvConf *= *true/generateResolvConf = false/' /etc/wsl.conf; " +
            "  elif grep -q '^\\[network\\]' /etc/wsl.conf 2>/dev/null; then sed -i '/^\\[network\\]/a generateResolvConf = false' /etc/wsl.conf; " +
            "  else printf '\\n[network]\\ngenerateResolvConf = false\\n' >> /etc/wsl.conf; fi; " +
            "else printf '[network]\\ngenerateResolvConf = false\\n' > /etc/wsl.conf; fi;",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(45));

        if (!fix.IsSuccess)
        {
            _logSink.Warn($"WSL networking fixes were only partially applied. {CommandDetails(fix)}");
            AddWarningOnce(context, "WSL networking fixes did not fully apply; Docker pulls may need a retry.");
        }
    }

    private async Task<CommandResult> VerifyDockerWithRemediationAsync(
        string distro,
        InstallerContext context,
        int attempt,
        CancellationToken cancellationToken)
    {
        if (attempt > 1)
        {
            _logSink.Info($"Docker readiness verification attempt {attempt}/{DockerVerifyAttempts}");
        }

        var result = await CheckDockerReadyAsync(distro, cancellationToken);
        if (result.IsSuccess)
        {
            return result;
        }

        var output = CombinedOutput(result);
        if (WslOutputClassifier.LooksDnsIssue(output))
        {
            _logSink.Warn("Detected DNS resolver issue while verifying Docker. Applying WSL DNS fix and retrying.");
            AddWarningOnce(context, "Applied WSL DNS fix after Docker resolver failures.");
            await _recoveryService.ApplyDnsFixAsync(distro, cancellationToken);
            await _recoveryService.ResetWslAsync(cancellationToken);
            await EnableAndStartDockerAsync(distro, cancellationToken);
            await WaitForDockerDaemonAsync(distro, cancellationToken);
        }
        else if (WslOutputClassifier.LooksTlsOrCacheIssue(output))
        {
            _logSink.Warn("Detected transient TLS/cache issue while verifying Docker. Restarting WSL and Docker.");
            AddWarningOnce(context, "Performed WSL reset and Docker restart after transient TLS/cache errors.");
            await _recoveryService.ResetWslAsync(cancellationToken);
            await RestartDockerServiceAsync(distro, cancellationToken);
            await WaitForDockerDaemonAsync(distro, cancellationToken);
        }

        return result;
    }

    private async Task<bool> IsDockerCliPresentAsync(string distro, CancellationToken cancellationToken)
    {
        var check = await _executor.RunInDistroAsync(
            distro,
            "command -v docker >/dev/null 2>&1",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(15));
        return check.IsSuccess;
    }

    private Task<CommandResult> ReadInstallLogTailAsync(string distro, CancellationToken cancellationToken)
    {
        return _executor.RunInDistroAsync(
            distro,
            "tail -n 200 /tmp/pf-docker-install.log 2>/dev/null || true",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(20));
    }

    private async Task<string> CollectDockerDiagnosticsAsync(string distro, CancellationToken cancellationToken)
    {
        var status = await _executor.RunInDistroAsync(
            distro,
            "systemctl status docker --no-pager -n 20 2>/dev/null || service docker status 2>/dev/null || true",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(25));
        var journal = await _executor.RunInDistroAsync(
            distro,
            "journalctl -u docker -n 50 --no-pager 2>/dev/null || true",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(25));
        var installLog = await ReadInstallLogTailAsync(distro, cancellationToken);

        return
            $"status={Clip(status.StandardOutput)} {Clip(status.StandardError)} | " +
            $"journal={Clip(journal.StandardOutput)} {Clip(journal.StandardError)} | " +
            $"install-log={Clip(installLog.StandardOutput)}";
    }

    private static void AddWarningOnce(InstallerContext context, string warning)
    {
        if (!context.Warnings.Contains(warning, StringComparer.Ordinal))
        {
            context.Warnings.Add(warning);
        }
    }

    private static string CombinedOutput(CommandResult result)
    {
        return $"{result.StandardOutput}\n{result.StandardError}";
    }

    private static string CombineInstallOutput(CommandResult installResult, CommandResult installTail)
    {
        return $"{installResult.StandardOutput}\n{installResult.StandardError}\n{installTail.StandardOutput}\n{installTail.StandardError}";
    }

    private static string CommandDetails(CommandResult result)
    {
        return $"exit={result.ExitCode}, stderr={Clip(result.StandardError)}, stdout={Clip(result.StandardOutput)}";
    }

    private static string Clip(string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return "<empty>";
        }

        var flattened = value.Replace('\0', ' ').Replace('\r', ' ').Replace('\n', ' ').Trim();
        flattened = Regex.Replace(flattened, @"\s{2,}", " ");
        flattened = Regex.Replace(flattened, @"\b(?:[A-Za-z0-9]\s+){3,}[A-Za-z0-9]\b", m => m.Value.Replace(" ", string.Empty));
        return flattened.Length <= 800 ? flattened : $"{flattened[..800]}...";
    }
}
