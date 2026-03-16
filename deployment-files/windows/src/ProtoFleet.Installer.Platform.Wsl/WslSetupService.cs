using ProtoFleet.Installer.Core;
using System.Text.RegularExpressions;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class WslSetupService : IWslSetupService
{
    // Keep this aligned with the PowerShell installer baseline (1800s).
    private static readonly TimeSpan DistroInstallTimeout = TimeSpan.FromMinutes(30);
    private readonly WslCommandExecutor _executor;
    private readonly IDockerReadinessService _dockerReadinessService;
    private readonly ILogSink _logSink;

    public WslSetupService(
        WslCommandExecutor executor,
        IDockerReadinessService dockerReadinessService,
        ILogSink logSink)
    {
        _executor = executor;
        _dockerReadinessService = dockerReadinessService;
        _logSink = logSink;
    }

    public async Task<InstallerStepResult> EnsureReadyAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        context.Checkpoint = InstallerCheckpoint.WslSetup;
        var status = await _executor.RunWslAsync("--status", cancellationToken);
        if (LooksUpdateRequired(status.StandardOutput + Environment.NewLine + status.StandardError))
        {
            var update = await _executor.RunWslAsync("--update", cancellationToken);
            if (!update.IsSuccess)
            {
                return InstallerStepResult.Failed("WSL update is required but update command failed.");
            }

            return RebootRequired(context, "WSL update completed. System reboot is required before continuing.");
        }

        if (!status.IsSuccess || LooksNotInstalled(status.StandardOutput + Environment.NewLine + status.StandardError))
        {
            var install = await _executor.RunWslAsync("--install --no-launch", cancellationToken);
            if (!install.IsSuccess && LooksNoLaunchUnsupported(install))
            {
                _logSink.Warn("--no-launch not supported by this WSL version. Retrying without it.");
                install = await _executor.RunWslAsync("--install", cancellationToken);
            }

            if (!install.IsSuccess)
            {
                var details = CommandDetails(install);
                if (LooksRebootRequired(details))
                {
                    return RebootRequired(context, $"WSL installation requires a reboot before continuing. {details}");
                }

                return InstallerStepResult.Failed($"WSL is not installed and installation could not be started. {details}");
            }

            return RebootRequired(context, "WSL installation started. Reboot is required before continuing.");
        }

        var defaultVersion = await _executor.RunWslAsync("--set-default-version 2", cancellationToken);
        if (!defaultVersion.IsSuccess)
        {
            _logSink.Warn($"Unable to enforce WSL default version 2. Continuing. {CommandDetails(defaultVersion)}");
        }

        if (!string.IsNullOrWhiteSpace(context.SelectedDistro))
        {
            _logSink.Info($"Using resumed distro selection: {context.SelectedDistro}");
            var resumed = new DistroInfo(context.SelectedDistro!, true, 2);
            return await ContinueWithSelectedDistroAsync(context, resumed, cancellationToken);
        }

        var distroList = await _executor.RunWslAsync("-l -v", cancellationToken);
        List<DistroInfo> distros;
        if (!distroList.IsSuccess)
        {
            var combined = $"{distroList.StandardOutput}{Environment.NewLine}{distroList.StandardError}";
            if (LooksNoDistroInstalled(combined))
            {
                distros = new List<DistroInfo>();
            }
            else
            {
                var quietList = await _executor.RunWslAsync("--list --quiet", cancellationToken);
                if (!quietList.IsSuccess)
                {
                    return InstallerStepResult.Failed(
                        "Failed to list WSL distributions. " +
                        $"wsl -l -v output: {SanitizeForMessage(combined)} " +
                        $"wsl --list --quiet output: {SanitizeForMessage($"{quietList.StandardOutput}{Environment.NewLine}{quietList.StandardError}")}");
                }

                distros = ParseDistrosFromQuietList(quietList.StandardOutput);
            }
        }
        else
        {
            distros = ParseDistros(distroList.StandardOutput);
        }

        if (distros.Count == 0)
        {
            var quietExisting = await _executor.RunWslAsync("--list --quiet", cancellationToken);
            if (quietExisting.IsSuccess)
            {
                var quietDistros = ParseDistrosFromQuietList(quietExisting.StandardOutput);
                if (quietDistros.Count > 0)
                {
                    _logSink.Warn("wsl -l -v reported no distros, but --list --quiet returned installed distro(s). Continuing with discovered distro list.");
                    distros = quietDistros;
                }
            }
        }

        if (distros.Count == 0)
        {
            var directInstallResult = await TryInstallDistroAsync("Ubuntu", useWebDownload: false, cancellationToken);
            var installUbuntu = directInstallResult.SuccessfulAttempt;
            var fallbackInstall = directInstallResult.LastAttempt;
            var distroAppeared = directInstallResult.DistroAppeared;
            var discoveredDistros = directInstallResult.DiscoveredDistros;
            if (installUbuntu is null && !distroAppeared && ShouldTryWebCatalogFallback(fallbackInstall))
            {
                // Some WSL versions accept only specific install flag variants or concrete distro IDs.
                _logSink.Warn("Ubuntu install via default source failed. Retrying with web catalog distro names.");
                var fallbackInstallResult = await InstallUbuntuFromWebCatalogAsync(cancellationToken);
                installUbuntu = fallbackInstallResult.SuccessfulAttempt;
                fallbackInstall = fallbackInstallResult.LastAttempt;
                if (fallbackInstallResult.DistroAppeared)
                {
                    distroAppeared = true;
                    discoveredDistros = fallbackInstallResult.DiscoveredDistros;
                }
            }
            else if (installUbuntu is null && !distroAppeared)
            {
                _logSink.Warn("Skipping web catalog fallback because failure does not indicate distro-name resolution issue.");
            }

            if (installUbuntu is null && !distroAppeared)
            {
                var finalRetryList = await _executor.RunWslAsync("--list --quiet", cancellationToken);
                if (finalRetryList.IsSuccess)
                {
                    var finalDistros = ParseDistrosFromQuietList(finalRetryList.StandardOutput);
                    if (finalDistros.Count > 0)
                    {
                        var selectedAfterDelay = SelectDistro(finalDistros);
                        _logSink.Warn(
                            $"Ubuntu install command did not return success, but distro '{selectedAfterDelay.Name}' is now available. Continuing.");
                        return await ContinueWithSelectedDistroAsync(context, selectedAfterDelay, cancellationToken);
                    }
                }

                var details = fallbackInstall is not null ? CommandDetails(fallbackInstall) : "No command output captured.";
                if (LooksRebootRequired(details))
                {
                    return RebootRequired(context, $"Ubuntu installation requires reboot before continuing. {details}");
                }

                return InstallerStepResult.Failed($"No WSL distro is installed and Ubuntu installation failed. {details}");
            }

            if (distroAppeared && installUbuntu is null)
            {
                var selectedFromRetry = SelectDistro(discoveredDistros);
                _logSink.Warn(
                    $"WSL install returned non-zero but distro '{selectedFromRetry.Name}' now exists. " +
                    $"Continuing with that distro.");
                return await ContinueWithSelectedDistroAsync(context, selectedFromRetry, cancellationToken);
            }

            var installDetails = CommandDetails(installUbuntu!);
            if (LooksRebootRequired(installDetails))
            {
                return RebootRequired(context, $"Ubuntu installation started and requires reboot. {installDetails}");
            }

            var retryList = await _executor.RunWslAsync("--list --quiet", cancellationToken);
            if (retryList.IsSuccess)
            {
                var retryDistros = ParseDistrosFromQuietList(retryList.StandardOutput);
                if (retryDistros.Count > 0)
                {
                    var selectedFromInstall = SelectDistro(retryDistros);
                    return await ContinueWithSelectedDistroAsync(context, selectedFromInstall, cancellationToken);
                }
            }

            return InstallerStepResult.Failed($"Ubuntu installation did not report a usable distro yet. {installDetails}");
        }

        var selected = SelectDistro(distros);
        return await ContinueWithSelectedDistroAsync(context, selected, cancellationToken);
    }

    private static bool LooksNotInstalled(string output)
    {
        if (string.IsNullOrWhiteSpace(output))
        {
            return true;
        }

        return output.Contains("not installed", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("requires an update", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Windows Subsystem for Linux has not been enabled", StringComparison.OrdinalIgnoreCase);
    }

    private static bool LooksNoDistroInstalled(string output)
    {
        if (string.IsNullOrWhiteSpace(output))
        {
            return false;
        }

        return output.Contains("no installed distributions", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("has no installed distributions", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("wsl --install", StringComparison.OrdinalIgnoreCase);
    }

    private static bool LooksUpdateRequired(string output)
    {
        if (string.IsNullOrWhiteSpace(output))
        {
            return false;
        }

        // Standard pattern: "WSL update required" style messages.
        if (output.Contains("update", StringComparison.OrdinalIgnoreCase) &&
            output.Contains("wsl", StringComparison.OrdinalIgnoreCase) &&
            output.Contains("required", StringComparison.OrdinalIgnoreCase))
        {
            return true;
        }

        // Older WSL on Win10: "The WSL2 kernel file is not found"
        if (output.Contains("kernel", StringComparison.OrdinalIgnoreCase) &&
            output.Contains("not found", StringComparison.OrdinalIgnoreCase))
        {
            return true;
        }

        // Older WSL on Win10: "please run 'wsl --update'"
        if (output.Contains("wsl --update", StringComparison.OrdinalIgnoreCase))
        {
            return true;
        }

        return false;
    }

    private static bool LooksRebootRequired(string output)
    {
        if (string.IsNullOrWhiteSpace(output))
        {
            return false;
        }

        return output.Contains("reboot", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("restart", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("please restart", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("changes will not be effective until", StringComparison.OrdinalIgnoreCase);
    }

    private static bool LooksAlreadyInstalled(string output)
    {
        if (string.IsNullOrWhiteSpace(output))
        {
            return false;
        }

        return output.Contains("already installed", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("already exists", StringComparison.OrdinalIgnoreCase);
    }

    private static bool LooksNoLaunchUnsupported(CommandResult result)
    {
        var output = $"{result.StandardOutput}{Environment.NewLine}{result.StandardError}";
        return output.Contains("unrecognized option", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("unknown option", StringComparison.OrdinalIgnoreCase);
    }

    private static bool ShouldTryWebCatalogFallback(CommandResult? result)
    {
        if (result is null)
        {
            return false;
        }

        var output = $"{result.StandardOutput}{Environment.NewLine}{result.StandardError}";
        if (string.IsNullOrWhiteSpace(output))
        {
            return false;
        }

        // Web catalog fallback is only useful when the distro identifier itself is rejected.
        return output.Contains("distribution", StringComparison.OrdinalIgnoreCase) &&
               (output.Contains("not found", StringComparison.OrdinalIgnoreCase) ||
                output.Contains("is not available", StringComparison.OrdinalIgnoreCase) ||
                output.Contains("invalid", StringComparison.OrdinalIgnoreCase) ||
                output.Contains("unknown", StringComparison.OrdinalIgnoreCase));
    }

    private static List<DistroInfo> ParseDistros(string output)
    {
        var result = new List<DistroInfo>();
        foreach (var rawLine in output.Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
        {
            var normalizedLine = NormalizeWslSpacing(rawLine);
            if (normalizedLine.StartsWith("NAME", StringComparison.OrdinalIgnoreCase))
            {
                continue;
            }

            var isDefault = normalizedLine.TrimStart().StartsWith('*');
            var line = normalizedLine.Replace("*", string.Empty).Trim();
            var parts = line.Split(' ', StringSplitOptions.RemoveEmptyEntries);
            if (parts.Length < 2)
            {
                continue;
            }

            if (!int.TryParse(parts[^1], out var version))
            {
                continue;
            }

            var name = string.Join(" ", parts.Take(parts.Length - 2));
            if (string.IsNullOrWhiteSpace(name))
            {
                name = parts[0];
            }

            result.Add(new DistroInfo(NormalizeWslSpacing(name), isDefault, version));
        }

        return result;
    }

    private static List<DistroInfo> ParseDistrosFromQuietList(string output)
    {
        var result = new List<DistroInfo>();
        foreach (var line in output.Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
        {
            var name = NormalizeWslSpacing(line.Trim().TrimStart('*').Trim());
            if (string.IsNullOrWhiteSpace(name))
            {
                continue;
            }

            // Quiet output does not include version, assume WSL2 and validate later if needed.
            result.Add(new DistroInfo(name, false, 2));
        }

        return result;
    }

    private static DistroInfo SelectDistro(IReadOnlyList<DistroInfo> distros)
    {
        return distros.FirstOrDefault(x => x.IsDefault)
            ?? distros.FirstOrDefault(x => x.Name.StartsWith("Ubuntu", StringComparison.OrdinalIgnoreCase))
            ?? distros[0];
    }

    private static string SanitizeForMessage(string value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return "<empty>";
        }

        var flattened = value.Replace('\r', ' ').Replace('\n', ' ').Trim();
        flattened = NormalizeWslSpacing(flattened);
        return flattened.Length <= 800 ? flattened : $"{flattened[..800]}...";
    }

    private static string NormalizeWslSpacing(string value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return value;
        }

        var cleaned = value.Replace('\0', ' ');
        cleaned = Regex.Replace(cleaned, @"\b(?:[A-Za-z0-9]\s+){3,}[A-Za-z0-9]\b", m => m.Value.Replace(" ", string.Empty));
        var tokens = cleaned.Split(' ', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);
        if (tokens.Length >= 4 && tokens.All(t => t.Length == 1 && char.IsLetterOrDigit(t[0])))
        {
            return string.Concat(tokens);
        }

        return cleaned;
    }

    private static string CommandDetails(CommandResult result)
    {
        return $"exit={result.ExitCode}, stderr={SanitizeForMessage(result.StandardError)}, stdout={SanitizeForMessage(result.StandardOutput)}";
    }

    private async Task<DistroInstallAttemptResult> TryInstallDistroAsync(
        string distroName,
        bool useWebDownload,
        CancellationToken cancellationToken)
    {
        var commands = BuildInstallCommands(distroName, useWebDownload);
        CommandResult? last = null;
        foreach (var command in commands)
        {
            var attempt = await _executor.RunWslAsync(command, cancellationToken, timeout: DistroInstallTimeout);
            if (attempt.IsSuccess)
            {
                return new DistroInstallAttemptResult(attempt, attempt, false, Array.Empty<DistroInfo>());
            }

            last = attempt;

            // Some WSL builds return non-zero while still registering the distro.
            var quiet = await _executor.RunWslAsync("--list --quiet", cancellationToken);
            if (quiet.IsSuccess)
            {
                var discovered = ParseDistrosFromQuietList(quiet.StandardOutput);
                if (discovered.Any(d => d.Name.StartsWith("Ubuntu", StringComparison.OrdinalIgnoreCase)))
                {
                    return new DistroInstallAttemptResult(null, attempt, true, discovered);
                }
            }
        }

        return new DistroInstallAttemptResult(null, last, false, Array.Empty<DistroInfo>());
    }

    private static IReadOnlyList<string> BuildInstallCommands(string distroName, bool useWebDownload)
    {
        var quoted = CommandEscaping.WindowsArgument(distroName);
        var web = useWebDownload ? "--web-download " : string.Empty;
        return new[]
        {
            $"--install {web}--no-launch -d {quoted}",
            $"--install {web}--no-launch --distribution {quoted}",
            $"--install {web}--no-launch {quoted}",
            // Fallback for older WSL versions that do not support --no-launch.
            $"--install {web}-d {quoted}",
            $"--install {web}--distribution {quoted}",
            $"--install {web}{quoted}"
        };
    }

    private async Task<WebCatalogInstallResult> InstallUbuntuFromWebCatalogAsync(CancellationToken cancellationToken)
    {
        var online = await _executor.RunWslAsync("--list --online", cancellationToken);
        var candidates = new List<string>();
        if (online.IsSuccess)
        {
            var discovered = ParseOnlineDistros(online.StandardOutput);
            var ubuntuPreferred = discovered
                .Where(name => name.StartsWith("Ubuntu", StringComparison.OrdinalIgnoreCase))
                .OrderBy(name => UbuntuPriority(name))
                .ToList();
            candidates.AddRange(ubuntuPreferred);
        }

        // Fallback candidates if online catalog parsing is unavailable or empty.
        if (candidates.Count == 0)
        {
            candidates.AddRange(new[] { "Ubuntu-24.04", "Ubuntu-22.04", "Ubuntu-20.04", "Ubuntu" });
        }

        CommandResult? last = null;
        IReadOnlyList<DistroInfo> discoveredFromFailure = Array.Empty<DistroInfo>();
        foreach (var distroName in candidates.Distinct(StringComparer.OrdinalIgnoreCase))
        {
            var result = await TryInstallDistroAsync(distroName, useWebDownload: true, cancellationToken);
            if (result.SuccessfulAttempt is not null)
            {
                _logSink.Info($"Successfully started distro install with web catalog entry '{distroName}'.");
                return new WebCatalogInstallResult(result.SuccessfulAttempt, result.LastAttempt, false, Array.Empty<DistroInfo>());
            }

            if (result.DistroAppeared)
            {
                return new WebCatalogInstallResult(null, result.LastAttempt, true, result.DiscoveredDistros);
            }

            last = result.LastAttempt;
            discoveredFromFailure = result.DiscoveredDistros;
        }

        return new WebCatalogInstallResult(null, last, false, discoveredFromFailure);
    }

    private static IReadOnlyList<string> ParseOnlineDistros(string output)
    {
        var result = new List<string>();
        foreach (var line in output.Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
        {
            if (line.StartsWith("NAME", StringComparison.OrdinalIgnoreCase) ||
                line.StartsWith("The following is", StringComparison.OrdinalIgnoreCase))
            {
                continue;
            }

            var parts = line.Split(' ', StringSplitOptions.RemoveEmptyEntries);
            if (parts.Length == 0)
            {
                continue;
            }

            var name = parts[0].Trim();
            if (!string.IsNullOrWhiteSpace(name))
            {
                result.Add(NormalizeWslSpacing(name));
            }
        }

        return result;
    }

    private static int UbuntuPriority(string distroName)
    {
        if (distroName.StartsWith("Ubuntu-24", StringComparison.OrdinalIgnoreCase))
        {
            return 0;
        }

        if (distroName.StartsWith("Ubuntu-22", StringComparison.OrdinalIgnoreCase))
        {
            return 1;
        }

        if (distroName.StartsWith("Ubuntu-20", StringComparison.OrdinalIgnoreCase))
        {
            return 2;
        }

        if (distroName.Equals("Ubuntu", StringComparison.OrdinalIgnoreCase))
        {
            return 3;
        }

        return 4;
    }

    private async Task<InstallerStepResult> ContinueWithSelectedDistroAsync(
        InstallerContext context,
        DistroInfo selected,
        CancellationToken cancellationToken)
    {
        var resolvedDistroName = await ResolveInstalledDistroNameAsync(selected.Name, cancellationToken);
        if (!resolvedDistroName.Equals(selected.Name, StringComparison.OrdinalIgnoreCase))
        {
            _logSink.Warn($"Adjusted distro selection from '{selected.Name}' to '{resolvedDistroName}' based on installed distro list.");
        }

        context.SelectedDistro = resolvedDistroName;
        _logSink.Info($"Selected distro: {resolvedDistroName}");

        var firstRunCheck = await FindLinuxUserAsync(resolvedDistroName, cancellationToken);
        var effectiveLinuxUser = firstRunCheck.Username;
        if (!firstRunCheck.Command.IsSuccess && string.IsNullOrWhiteSpace(effectiveLinuxUser))
        {
            var details = CommandDetails(firstRunCheck.Command);
            if (LooksRebootRequired(details))
            {
                return RebootRequired(context, $"WSL distro '{resolvedDistroName}' requires reboot before continuing. {details}");
            }
        }

        if (string.IsNullOrWhiteSpace(effectiveLinuxUser))
        {
            // Match the PowerShell installer behavior: complete first-run user setup interactively.
            context.LinuxUserVerified = false;
            context.LinuxUserVerifiedAt = null;
            context.Checkpoint = InstallerCheckpoint.LinuxUserProvisioning;
            return InstallerStepResult.AwaitUserAction(
                InstallerUserActionType.WaitForLinuxUserSetup,
                $"Ubuntu first-run setup is required for '{resolvedDistroName}'. " +
                "Create the Linux username/password in the Ubuntu window, then the installer will continue automatically.",
                resolvedDistroName);
        }

        context.LinuxUserVerified = true;
        context.LinuxUserVerifiedAt = DateTimeOffset.Now;
        context.LinuxProvisionedUsername = effectiveLinuxUser;
        context.LinuxCredentialFilePath = null;

        if (selected.Version == 1)
        {
            var setVersion = await _executor.RunWslAsync(
                $"--set-version {CommandEscaping.WindowsArgument(resolvedDistroName)} 2",
                cancellationToken);
            if (!setVersion.IsSuccess)
            {
                return InstallerStepResult.Failed($"Failed to upgrade distro '{resolvedDistroName}' to WSL2. {CommandDetails(setVersion)}");
            }
        }

        var wslConfScript =
            "set -e; " +
            "if [ ! -f /etc/wsl.conf ]; then touch /etc/wsl.conf; fi; " +
            "grep -q '^\\[automount\\]' /etc/wsl.conf || printf '\\n[automount]\\nenabled=true\\n' >> /etc/wsl.conf; " +
            "grep -q '^\\[boot\\]' /etc/wsl.conf || printf '\\n[boot]\\nsystemd=true\\n' >> /etc/wsl.conf; " +
            BuildDefaultUserConfigScript(effectiveLinuxUser);

        var confResult = await _executor.RunInDistroAsync(resolvedDistroName, wslConfScript, asRoot: true, cancellationToken);
        if (!confResult.IsSuccess)
        {
            _logSink.Warn($"Could not fully apply /etc/wsl.conf settings. {CommandDetails(confResult)}");
        }

        await _executor.RunWslAsync("--shutdown", cancellationToken);
        context.Checkpoint = InstallerCheckpoint.DockerSetup;
        var dockerResult = await _dockerReadinessService.EnsureReadyAsync(context, cancellationToken);
        if (!dockerResult.Success)
        {
            return dockerResult;
        }

        context.Checkpoint = InstallerCheckpoint.DeploymentPreparation;
        return InstallerStepResult.Succeeded();
    }

    private async Task<string> ResolveInstalledDistroNameAsync(string preferredName, CancellationToken cancellationToken)
    {
        var preferred = NormalizeWslSpacing(preferredName).Trim();
        if (string.IsNullOrWhiteSpace(preferred))
        {
            preferred = "Ubuntu";
        }

        var quiet = await _executor.RunWslAsync("--list --quiet", cancellationToken);
        if (!quiet.IsSuccess)
        {
            return preferred;
        }

        var installed = ParseDistrosFromQuietList(quiet.StandardOutput)
            .Select(x => NormalizeWslSpacing(x.Name).Trim())
            .Where(x => !string.IsNullOrWhiteSpace(x))
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .ToList();

        if (installed.Count == 0)
        {
            return preferred;
        }

        var exact = installed.FirstOrDefault(x => x.Equals(preferred, StringComparison.OrdinalIgnoreCase));
        if (!string.IsNullOrWhiteSpace(exact))
        {
            return exact;
        }

        static string Key(string value) => Regex.Replace(value, "\\s+", string.Empty).ToLowerInvariant();
        var preferredKey = Key(preferred);
        var normalizedMatch = installed.FirstOrDefault(x => Key(x) == preferredKey);
        if (!string.IsNullOrWhiteSpace(normalizedMatch))
        {
            return normalizedMatch;
        }

        if (preferred.Contains("ubuntu", StringComparison.OrdinalIgnoreCase))
        {
            var ubuntu = installed.FirstOrDefault(x => x.StartsWith("Ubuntu", StringComparison.OrdinalIgnoreCase));
            if (!string.IsNullOrWhiteSpace(ubuntu))
            {
                return ubuntu;
            }
        }

        return installed[0];
    }

    private static string BuildDefaultUserConfigScript(string? linuxUsername)
    {
        if (string.IsNullOrWhiteSpace(linuxUsername))
        {
            return string.Empty;
        }

        var escaped = EscapeForSingleQuotedShell(linuxUsername);
        return
            $" USER_NAME='{escaped}';" +
            " grep -q '^\\[user\\]' /etc/wsl.conf || printf '\\n[user]\\n' >> /etc/wsl.conf;" +
            " if grep -q '^default=' /etc/wsl.conf; then sed -i \"s/^default=.*/default=${USER_NAME}/\" /etc/wsl.conf; " +
            "else printf 'default=%s\\n' \"$USER_NAME\" >> /etc/wsl.conf; fi;";
    }

    private async Task<(string? Username, CommandResult Command)> FindLinuxUserAsync(string distro, CancellationToken cancellationToken)
    {
        var check = await _executor.RunInDistroAsync(
            distro,
            "getent passwd 1000 | cut -d: -f1",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(20));

        if (!check.IsSuccess || string.IsNullOrWhiteSpace(check.StandardOutput))
        {
            return (null, check);
        }

        var username = check.StandardOutput
            .Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
            .FirstOrDefault();
        return (string.IsNullOrWhiteSpace(username) ? null : username, check);
    }

    private static string EscapeForSingleQuotedShell(string value)
    {
        return value.Replace("'", "'\"'\"'");
    }

    private sealed record DistroInfo(string Name, bool IsDefault, int Version);
    private sealed record DistroInstallAttemptResult(
        CommandResult? SuccessfulAttempt,
        CommandResult? LastAttempt,
        bool DistroAppeared,
        IReadOnlyList<DistroInfo> DiscoveredDistros);
    private sealed record WebCatalogInstallResult(
        CommandResult? SuccessfulAttempt,
        CommandResult? LastAttempt,
        bool DistroAppeared,
        IReadOnlyList<DistroInfo> DiscoveredDistros);

    private static InstallerStepResult RebootRequired(InstallerContext context, string message)
    {
        context.RebootRequired = true;
        context.Checkpoint = InstallerCheckpoint.WslSetup;
        return InstallerStepResult.Failed(message, InstallerExitCode.RebootRequired);
    }
}
