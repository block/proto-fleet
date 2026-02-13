using System.Diagnostics;
using System.Runtime.InteropServices;
using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Windows;

public sealed class SystemPrereqService : ISystemPrereqService
{
    private static readonly string[] RequiredWslFeatures =
    [
        "Microsoft-Windows-Subsystem-Linux",
        "VirtualMachinePlatform"
    ];

    public async Task<HostCheckReport> CheckHostAsync(CancellationToken cancellationToken)
    {
        cancellationToken.ThrowIfCancellationRequested();

        var warnings = new List<string>();
        var fatalMessages = new List<string>();
        var rebootReasons = new List<string>();
        var build = Environment.OSVersion.Version.Build;

        if (build < 19041)
        {
            fatalMessages.Add($"Unsupported Windows build {build}. Windows 10 build 19041+ is required.");
        }

        var totalMemory = GetTotalPhysicalMemory();
        if (totalMemory < 8UL * 1024UL * 1024UL * 1024UL)
        {
            warnings.Add($"System memory is below recommended minimum (8 GB). Current: {totalMemory / 1024 / 1024 / 1024} GB.");
        }

        var cDrive = DriveInfo.GetDrives().FirstOrDefault(d => d.Name.Equals("C:\\", StringComparison.OrdinalIgnoreCase));
        var free = cDrive?.AvailableFreeSpace ?? 0;
        if (free < 20L * 1024L * 1024L * 1024L)
        {
            warnings.Add($"C: drive free space is below recommended minimum (20 GB). Current: {free / 1024 / 1024 / 1024} GB.");
        }

        var featureChecks = await QueryRequiredFeatureStatesAsync(cancellationToken);
        var featuresToEnable = featureChecks
            .Where(x => CanAutoEnableState(NormalizeState(x.State)))
            .ToList();

        var enableFailures = new List<string>();
        var restartRequiredByEnable = false;
        foreach (var feature in featuresToEnable)
        {
            cancellationToken.ThrowIfCancellationRequested();
            var enableResult = await TryEnableFeatureAsync(feature.Name, cancellationToken);
            if (!enableResult.Success)
            {
                enableFailures.Add($"Failed to enable '{feature.Name}': {enableResult.ErrorMessage}");
                continue;
            }

            restartRequiredByEnable |= enableResult.RestartNeeded;
        }

        if (enableFailures.Count > 0)
        {
            fatalMessages.AddRange(enableFailures);
        }

        if (featuresToEnable.Count > 0)
        {
            featureChecks = await QueryRequiredFeatureStatesAsync(cancellationToken);
            if (restartRequiredByEnable)
            {
                rebootReasons.Add("WSL Windows features were enabled and a reboot is required.");
            }
        }

        var blockingFeatures = featureChecks.Where(x => x.IsBlocking).ToList();
        var pendingFeatures = blockingFeatures
            .Where(x => IsPendingState(NormalizeState(x.State)))
            .ToList();
        if (pendingFeatures.Count > 0)
        {
            rebootReasons.Add("A system restart is required for pending WSL Windows feature changes.");
        }

        var unresolvedBlocking = blockingFeatures
            .Where(x => !IsPendingState(NormalizeState(x.State)))
            .ToList();
        if (unresolvedBlocking.Count > 0)
        {
            fatalMessages.Add("WSL prerequisites are not ready.");
            foreach (var feature in unresolvedBlocking)
            {
                var remediation = string.IsNullOrWhiteSpace(feature.RemediationMessage)
                    ? "Review the feature state and retry."
                    : feature.RemediationMessage;
                fatalMessages.Add($"Windows feature '{feature.Name}' is '{feature.State}'. {remediation}");
            }
        }

        var requiresReboot = fatalMessages.Count == 0 && rebootReasons.Count > 0;
        var rebootMessage = requiresReboot
            ? string.Join(Environment.NewLine, rebootReasons.Distinct(StringComparer.Ordinal))
            : null;

        return new HostCheckReport
        {
            IsSupportedBuild = build >= 19041,
            WindowsBuild = build,
            TotalMemoryBytes = totalMemory,
            CDriveFreeBytes = free,
            WindowsFeatureChecks = featureChecks,
            Warnings = warnings,
            RequiresReboot = requiresReboot,
            RebootMessage = rebootMessage,
            FatalError = fatalMessages.Count > 0 ? string.Join(Environment.NewLine, fatalMessages) : null,
        };
    }

    private static bool IsEnabledState(string normalizedState)
    {
        return normalizedState == "enabled";
    }

    private static bool IsPendingState(string normalizedState)
    {
        return normalizedState is "enablepending" or "disablepending";
    }

    private static bool CanAutoEnableState(string normalizedState)
    {
        return normalizedState is "disabled" or "disabledwithpayloadremoved";
    }

    private static string BuildRemediationMessage(string featureName, string normalizedState)
    {
        if (IsPendingState(normalizedState))
        {
            return "A system restart is required for pending Windows feature changes.";
        }

        if (CanAutoEnableState(normalizedState))
        {
            return $"Enable '{featureName}' and reboot before continuing.";
        }

        return "Set this feature to Enabled and reboot if prompted before retrying.";
    }

    private static string NormalizeState(string value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return string.Empty;
        }

        return new string(value.Where(char.IsLetter).Select(char.ToLowerInvariant).ToArray());
    }

    private static async Task<List<WindowsFeatureCheck>> QueryRequiredFeatureStatesAsync(CancellationToken cancellationToken)
    {
        var featureChecks = new List<WindowsFeatureCheck>(RequiredWslFeatures.Length);
        foreach (var featureName in RequiredWslFeatures)
        {
            cancellationToken.ThrowIfCancellationRequested();
            featureChecks.Add(await QueryFeatureStateAsync(featureName, cancellationToken));
        }

        return featureChecks;
    }

    private static async Task<WindowsFeatureCheck> QueryFeatureStateAsync(string featureName, CancellationToken cancellationToken)
    {
        var command = $"""(Get-WindowsOptionalFeature -Online -FeatureName '{featureName}' -ErrorAction Stop).State""";
        var result = await RunPowerShellAsync(command, cancellationToken);
        if (!result.Success)
        {
            return new WindowsFeatureCheck
            {
                Name = featureName,
                State = "Unknown",
                IsBlocking = true,
                RemediationMessage =
                    $"Failed to query feature state ({result.ErrorMessage}). Re-run installer as administrator and ensure the feature state can be read.",
            };
        }

        var state = result.Output;
        var normalizedState = NormalizeState(state);
        var isBlocking = !IsEnabledState(normalizedState);

        return new WindowsFeatureCheck
        {
            Name = featureName,
            State = state,
            IsBlocking = isBlocking,
            RemediationMessage = isBlocking ? BuildRemediationMessage(featureName, normalizedState) : null,
        };
    }

    private static async Task<(bool Success, bool RestartNeeded, string ErrorMessage)> TryEnableFeatureAsync(
        string featureName,
        CancellationToken cancellationToken)
    {
        var command =
            $"$result = Enable-WindowsOptionalFeature -Online -FeatureName '{featureName}' -NoRestart -All -ErrorAction Stop; if ($result.RestartNeeded) {{ 'RestartNeeded' }} else {{ 'NoRestartNeeded' }}";
        var result = await RunPowerShellAsync(command, cancellationToken);
        if (!result.Success)
        {
            return (false, false, result.ErrorMessage);
        }

        var normalized = NormalizeState(result.Output);
        var restartNeeded = normalized.Contains("restartneeded", StringComparison.Ordinal);
        return (true, restartNeeded, string.Empty);
    }

    private static async Task<(bool Success, string Output, string ErrorMessage)> RunPowerShellAsync(
        string command,
        CancellationToken cancellationToken)
    {
        var startInfo = new ProcessStartInfo
        {
            FileName = "powershell.exe",
            Arguments = $"-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command \"{command}\"",
            UseShellExecute = false,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            CreateNoWindow = true,
        };

        using var process = new Process { StartInfo = startInfo };
        try
        {
            if (!process.Start())
            {
                return (false, string.Empty, "failed to start powershell.exe");
            }

            var stdOutTask = process.StandardOutput.ReadToEndAsync();
            var stdErrTask = process.StandardError.ReadToEndAsync();

            using var timeoutCts = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
            timeoutCts.CancelAfter(TimeSpan.FromSeconds(20));

            try
            {
                await process.WaitForExitAsync(timeoutCts.Token);
            }
            catch (OperationCanceledException) when (!cancellationToken.IsCancellationRequested)
            {
                try
                {
                    if (!process.HasExited)
                    {
                        process.Kill(entireProcessTree: true);
                    }
                }
                catch
                {
                    // Ignore kill failures during timeout cleanup.
                }

                return (false, string.Empty, "timed out while reading Windows feature state");
            }

            var stdout = (await stdOutTask).Trim();
            var stderr = (await stdErrTask).Trim();
            if (process.ExitCode != 0)
            {
                return (false, stdout, string.IsNullOrWhiteSpace(stderr) ? $"exit code {process.ExitCode}" : stderr);
            }

            if (string.IsNullOrWhiteSpace(stdout))
            {
                return (false, string.Empty, "feature query returned empty state");
            }

            var firstLine = stdout
                .Split(Environment.NewLine, StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
                .FirstOrDefault();

            return string.IsNullOrWhiteSpace(firstLine)
                ? (false, string.Empty, "feature query returned empty state")
                : (true, firstLine!, string.Empty);
        }
        catch (OperationCanceledException)
        {
            throw;
        }
        catch (Exception ex)
        {
            return (false, string.Empty, ex.Message);
        }
    }

    private static ulong GetTotalPhysicalMemory()
    {
        var status = new MemoryStatusEx();
        return GlobalMemoryStatusEx(status) ? status.TotalPhysicalMemory : 0;
    }

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool GlobalMemoryStatusEx([In, Out] MemoryStatusEx status);

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Auto)]
    private sealed class MemoryStatusEx
    {
        public uint Length = (uint)Marshal.SizeOf<MemoryStatusEx>();
        public uint MemoryLoad;
        public ulong TotalPhysicalMemory;
        public ulong AvailablePhysicalMemory;
        public ulong TotalPageFile;
        public ulong AvailablePageFile;
        public ulong TotalVirtual;
        public ulong AvailableVirtual;
        public ulong AvailableExtendedVirtual;
    }
}
