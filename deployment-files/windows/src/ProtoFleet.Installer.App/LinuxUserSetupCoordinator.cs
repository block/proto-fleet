using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Platform.Wsl;

namespace ProtoFleet.Installer.App;

public sealed class LinuxUserSetupCoordinator : IDisposable
{
    private readonly Func<WslCommandExecutor> _executorFactory;
    private readonly Action<string> _appendLogLine;
    private readonly TimeSpan _pollInterval;
    private readonly TimeSpan _probeTimeout;

    private readonly object _sync = new();
    private CancellationTokenSource? _pollCts;
    private string? _waitingDistro;
    private InstallerOptions? _pendingOptions;
    private bool _probeInProgress;
    private bool _resumeTriggered;

    public LinuxUserSetupCoordinator(
        Func<WslCommandExecutor> executorFactory,
        Action<string> appendLogLine,
        TimeSpan pollInterval,
        TimeSpan probeTimeout)
    {
        _executorFactory = executorFactory;
        _appendLogLine = appendLogLine;
        _pollInterval = pollInterval;
        _probeTimeout = probeTimeout;
    }

    public event Action<string>? StatusChanged;

    public event Func<string, InstallerOptions, Task>? ResumeRequested;

    public string? WaitingDistro
    {
        get
        {
            lock (_sync)
            {
                return _waitingDistro;
            }
        }
    }

    public void BeginWait(string distro, InstallerOptions options, string? initialStatus, int launchRetryLimit)
    {
        Cancel();

        lock (_sync)
        {
            _waitingDistro = distro;
            _pendingOptions = options;
            _resumeTriggered = false;
            _probeInProgress = false;
            _pollCts = new CancellationTokenSource();
        }

        StatusChanged?.Invoke(initialStatus ?? "Waiting for Linux user creation in distro setup...");
        _ = LaunchUbuntuWindowWithRetryAsync(distro, launchRetryLimit, CancellationToken.None);
        _ = PollForLinuxUserSetupAsync();
    }

    public async Task OpenUbuntuWindowAsync(int launchRetryLimit)
    {
        var distro = WaitingDistro;
        if (string.IsNullOrWhiteSpace(distro))
        {
            return;
        }

        await LaunchUbuntuWindowWithRetryAsync(distro, launchRetryLimit, CancellationToken.None);
    }

    public async Task CheckNowAsync()
    {
        var (distro, options) = SnapshotPending();
        if (string.IsNullOrWhiteSpace(distro) || options is null)
        {
            return;
        }

        StatusChanged?.Invoke("Verifying Linux user setup now...");
        await TryProbeAndResumeAsync(distro, options, initiatedByUser: true, CancellationToken.None);
    }

    public string? GetManualCommand()
    {
        var distro = WaitingDistro;
        if (string.IsNullOrWhiteSpace(distro))
        {
            return null;
        }

        var distroArg = distro.Contains(' ') ? $"\"{distro}\"" : distro;
        return $"wsl.exe -d {distroArg}";
    }

    public void Cancel()
    {
        lock (_sync)
        {
            _pollCts?.Cancel();
            _pollCts?.Dispose();
            _pollCts = null;
            _waitingDistro = null;
            _pendingOptions = null;
            _probeInProgress = false;
            _resumeTriggered = false;
        }
    }

    private async Task PollForLinuxUserSetupAsync()
    {
        var token = GetPollToken();
        if (token == CancellationToken.None)
        {
            return;
        }

        while (!token.IsCancellationRequested)
        {
            try
            {
                var (distro, options) = SnapshotPending();
                if (string.IsNullOrWhiteSpace(distro) || options is null)
                {
                    return;
                }

                var resumed = await TryProbeAndResumeAsync(
                    distro,
                    options,
                    initiatedByUser: false,
                    token);
                if (resumed)
                {
                    return;
                }
            }
            catch (OperationCanceledException)
            {
                return;
            }
            catch (Exception ex)
            {
                _appendLogLine($"[WARN] Linux-user probe error: {ex.Message}");
                StatusChanged?.Invoke(
                    "Still waiting for Linux user setup. If no Ubuntu window is visible, open Need help? and click Open Ubuntu Window.");
            }

            await Task.Delay(_pollInterval, token);
        }
    }

    private async Task<bool> TryProbeAndResumeAsync(
        string distro,
        InstallerOptions options,
        bool initiatedByUser,
        CancellationToken cancellationToken)
    {
        lock (_sync)
        {
            if (_resumeTriggered)
            {
                return true;
            }

            if (_probeInProgress)
            {
                return false;
            }

            _probeInProgress = true;
        }

        try
        {
            var executor = _executorFactory();
            var check = await executor.RunInDistroAsync(
                distro,
                "getent passwd 1000 | cut -d: -f1",
                asRoot: true,
                cancellationToken,
                timeout: _probeTimeout);

            if (check.IsSuccess && !string.IsNullOrWhiteSpace(check.StandardOutput))
            {
                var username = check.StandardOutput.Trim()
                    .Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)
                    .FirstOrDefault() ?? "<unknown>";

                StatusChanged?.Invoke($"Detected Linux user '{username}'. Verifying and resuming installer...");
                lock (_sync)
                {
                    _resumeTriggered = true;
                }

                CancelPollingOnly();
                var resumeHandler = ResumeRequested;
                if (resumeHandler is not null)
                {
                    await resumeHandler.Invoke(distro, options);
                }

                return true;
            }

            StatusChanged?.Invoke(
                initiatedByUser
                    ? "Linux user not detected yet. Finish setup in Ubuntu, then click 'Check Setup' again."
                    : "Linux user not detected yet. Complete username/password creation in the Ubuntu window.");
            return false;
        }
        finally
        {
            lock (_sync)
            {
                _probeInProgress = false;
            }
        }
    }

    private async Task LaunchUbuntuWindowWithRetryAsync(string distro, int maxAttempts, CancellationToken cancellationToken)
    {
        var attempts = Math.Max(1, maxAttempts);
        for (var attempt = 1; attempt <= attempts; attempt++)
        {
            cancellationToken.ThrowIfCancellationRequested();

            var launched = _executorFactory().LaunchInteractiveDistro(distro);
            if (launched)
            {
                StatusChanged?.Invoke("Ubuntu setup window opened. Complete username/password setup there.");
                return;
            }

            if (attempt < attempts)
            {
                StatusChanged?.Invoke($"Ubuntu window launch failed (attempt {attempt}/{attempts}). Retrying...");
                await Task.Delay(TimeSpan.FromSeconds(2), cancellationToken);
            }
        }

        StatusChanged?.Invoke(
            "Could not open Ubuntu window automatically after retries. Open Need help? and use Copy command to run it manually.");
    }

    private (string? Distro, InstallerOptions? Options) SnapshotPending()
    {
        lock (_sync)
        {
            return (_waitingDistro, _pendingOptions);
        }
    }

    private CancellationToken GetPollToken()
    {
        lock (_sync)
        {
            return _pollCts?.Token ?? CancellationToken.None;
        }
    }

    private void CancelPollingOnly()
    {
        lock (_sync)
        {
            _pollCts?.Cancel();
        }
    }

    public void Dispose()
    {
        Cancel();
    }
}
