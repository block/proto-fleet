using ProtoFleet.Installer.Core;
using System.Text.RegularExpressions;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class ComposeDeployer : IComposeDeployer
{
    private const int ComposePullAttempts = 5;
    private const int ComposeBuildAttempts = 5;
    private const int ComposeUpAttempts = 3;
    private readonly WslCommandExecutor _executor;
    private readonly IDockerReadinessService _dockerReadinessService;
    private readonly ILogSink _logSink;
    private readonly WslRecoveryService _recoveryService;

    public ComposeDeployer(
        WslCommandExecutor executor,
        IDockerReadinessService dockerReadinessService,
        ILogSink logSink)
    {
        _executor = executor;
        _dockerReadinessService = dockerReadinessService;
        _logSink = logSink;
        _recoveryService = new WslRecoveryService(executor, logSink);
    }

    public async Task<InstallerStepResult> DeployAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(context.DeploymentRootWslPath) || string.IsNullOrWhiteSpace(context.SelectedDistro))
        {
            return InstallerStepResult.Failed("Cannot deploy without WSL deployment path and selected distro.");
        }

        var dockerReady = await _dockerReadinessService.EnsureReadyAsync(context, cancellationToken);
        if (!dockerReady.Success)
        {
            return dockerReady;
        }

        var archResult = await _executor.RunInDistroAsync(context.SelectedDistro, "uname -m", asRoot: false, cancellationToken);
        if (!archResult.IsSuccess)
        {
            return InstallerStepResult.Failed($"Could not detect Linux architecture. {CommandDetails(archResult)}");
        }

        var arch = MapArch(archResult.StandardOutput);
        if (arch is null)
        {
            return InstallerStepResult.Failed($"Unsupported architecture reported by uname: {archResult.StandardOutput.Trim()}");
        }

        var tsdbImagePath = $"{context.DeploymentRootWslPath}/images/timescaledb.tar.gz";
        _logSink.Info($"Loading pre-built TimescaleDB image for {arch}...");
        var loadResult = await _executor.RunInDistroAsync(
            context.SelectedDistro,
            $"if [ -f {ShellEscaping.BashSingleQuote(tsdbImagePath)} ]; then gunzip -c {ShellEscaping.BashSingleQuote(tsdbImagePath)} | docker load; else echo 'Warning: Pre-built TimescaleDB image not found'; fi",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromMinutes(5));
        if (!loadResult.IsSuccess)
        {
            return InstallerStepResult.Failed($"Failed to load pre-built TimescaleDB image. {CommandDetails(loadResult)}");
        }

        var pull = await RetryPolicy.ExecuteAsync(
            attempts: ComposePullAttempts,
            action: async attempt =>
            {
                _logSink.Info($"docker compose pull attempt {attempt}/{ComposePullAttempts}");
                var result = await RunComposeAsync(
                    context,
                    $"export TARGETARCH={arch}; DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml pull",
                    cancellationToken,
                    timeout: TimeSpan.FromMinutes(8));
                if (!result.IsSuccess)
                {
                    await TryRemediateAsync(context, "pull", result, cancellationToken);
                }

                return result;
            },
            isSuccess: result => result.IsSuccess,
            backoff: attempt => TimeSpan.FromSeconds(Math.Min(30, Math.Pow(2, attempt))));

        if (!pull.IsSuccess)
        {
            return InstallerStepResult.Failed($"docker compose pull failed after retries. {CommandDetails(pull)}");
        }

        var build = await RetryPolicy.ExecuteAsync(
            attempts: ComposeBuildAttempts,
            action: async attempt =>
            {
                _logSink.Info($"docker compose build attempt {attempt}/{ComposeBuildAttempts}");
                var result = await RunComposeAsync(
                    context,
                    $"export TARGETARCH={arch}; DOCKER_CLI_PROGRESS=plain docker compose -f docker-compose.yaml build --no-cache",
                    cancellationToken,
                    timeout: TimeSpan.FromMinutes(20));

                if (!result.IsSuccess &&
                    !WslOutputClassifier.LooksFalseNegativeComposeBuild(CombinedOutput(result)))
                {
                    await TryRemediateAsync(context, "build", result, cancellationToken);
                }

                return result;
            },
            isSuccess: result => result.IsSuccess || WslOutputClassifier.LooksFalseNegativeComposeBuild(CombinedOutput(result)),
            backoff: attempt => TimeSpan.FromSeconds(Math.Min(30, Math.Pow(2, attempt))));

        if (!build.IsSuccess && !WslOutputClassifier.LooksFalseNegativeComposeBuild(CombinedOutput(build)))
        {
            return InstallerStepResult.Failed($"docker compose build failed after retries. {CommandDetails(build)}");
        }

        var down = await RunComposeAsync(
            context,
            "docker compose -f docker-compose.yaml down",
            cancellationToken,
            timeout: TimeSpan.FromMinutes(5));
        if (!down.IsSuccess)
        {
            _logSink.Warn($"docker compose down failed; continuing. {CommandDetails(down)}");
        }

        var up = await RetryPolicy.ExecuteAsync(
            attempts: ComposeUpAttempts,
            action: async attempt =>
            {
                _logSink.Info($"docker compose up attempt {attempt}/{ComposeUpAttempts}");
                var result = await RunComposeAsync(
                    context,
                    "docker compose -f docker-compose.yaml up -d",
                    cancellationToken,
                    timeout: TimeSpan.FromMinutes(6));
                if (!result.IsSuccess)
                {
                    await TryRemediateAsync(context, "up", result, cancellationToken);
                }

                return result;
            },
            isSuccess: result => result.IsSuccess,
            backoff: _ => TimeSpan.FromSeconds(5));

        if (!up.IsSuccess)
        {
            return InstallerStepResult.Failed($"docker compose up -d failed after retries. {CommandDetails(up)}");
        }

        return InstallerStepResult.Succeeded();
    }

    private Task<CommandResult> RunComposeAsync(
        InstallerContext context,
        string command,
        CancellationToken cancellationToken,
        TimeSpan? timeout = null)
    {
        var bash = $"cd {ShellEscaping.BashSingleQuote(context.DeploymentRootWslPath!)} && {command}";
        return _executor.RunInDistroAsync(
            context.SelectedDistro!,
            bash,
            asRoot: true,
            cancellationToken,
            timeout: timeout);
    }

    private async Task TryRemediateAsync(
        InstallerContext context,
        string phase,
        CommandResult failure,
        CancellationToken cancellationToken)
    {
        var output = CombinedOutput(failure);
        _logSink.Warn($"Compose {phase} failure detected. {CommandDetails(failure)}");

        if (WslOutputClassifier.LooksDockerCliMissing(output) ||
            WslOutputClassifier.LooksDockerDaemonUnavailable(output))
        {
            _logSink.Warn("Docker CLI/daemon not ready during compose operation. Re-running Docker readiness.");
            AddWarningOnce(context, $"Docker readiness remediation applied during compose {phase}.");
            await _dockerReadinessService.EnsureReadyAsync(context, cancellationToken);
            return;
        }

        if (WslOutputClassifier.LooksDnsIssue(output))
        {
            _logSink.Warn("Detected DNS resolver issue during compose operation. Applying WSL DNS fix.");
            AddWarningOnce(context, $"Applied WSL DNS fix during compose {phase}.");
            await _recoveryService.ApplyDnsFixAsync(context.SelectedDistro!, cancellationToken);
            await _recoveryService.ResetWslAsync(cancellationToken);
            await _dockerReadinessService.EnsureReadyAsync(context, cancellationToken);
            return;
        }

        if (WslOutputClassifier.LooksTlsOrCacheIssue(output))
        {
            _logSink.Warn("Detected transient TLS/cache issue during compose operation. Resetting WSL and restarting Docker.");
            AddWarningOnce(context, $"Applied WSL reset and Docker restart during compose {phase}.");
            await _recoveryService.ResetWslAsync(cancellationToken, TimeSpan.FromSeconds(3));
            await _executor.RunInDistroAsync(
                context.SelectedDistro!,
                "systemctl restart docker 2>/dev/null || service docker restart 2>/dev/null || /etc/init.d/docker restart 2>/dev/null || true",
                asRoot: true,
                cancellationToken,
                timeout: TimeSpan.FromSeconds(45));
            await _dockerReadinessService.EnsureReadyAsync(context, cancellationToken);
        }
    }

    private static string? MapArch(string unameOutput)
    {
        var value = unameOutput.Trim().ToLowerInvariant();
        return value switch
        {
            "x86_64" => "amd64",
            "amd64" => "amd64",
            "aarch64" => "arm64",
            "arm64" => "arm64",
            _ => null
        };
    }

    private static string CombinedOutput(CommandResult result)
    {
        return $"{result.StandardOutput}\n{result.StandardError}";
    }

    private static void AddWarningOnce(InstallerContext context, string warning)
    {
        if (!context.Warnings.Contains(warning, StringComparer.Ordinal))
        {
            context.Warnings.Add(warning);
        }
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
        return flattened.Length <= 800 ? flattened : $"{flattened[..800]}...";
    }
}
