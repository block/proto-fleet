using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class WslRecoveryService
{
    private readonly WslCommandExecutor _executor;
    private readonly ILogSink _logSink;

    public WslRecoveryService(WslCommandExecutor executor, ILogSink logSink)
    {
        _executor = executor;
        _logSink = logSink;
    }

    public async Task ApplyDnsFixAsync(string distro, CancellationToken cancellationToken)
    {
        var fix = await _executor.RunInDistroAsync(
            distro,
            "set -e; " +
            "if [ -L /etc/resolv.conf ]; then rm -f /etc/resolv.conf; fi; " +
            "cat > /etc/resolv.conf <<'EOF'\n" +
            "nameserver 1.1.1.1\n" +
            "nameserver 8.8.8.8\n" +
            "EOF\n" +
            "if [ -f /etc/wsl.conf ]; then " +
            "  if grep -q 'generateResolvConf' /etc/wsl.conf; then sed -i 's/generateResolvConf *= *true/generateResolvConf = false/' /etc/wsl.conf; " +
            "  elif grep -q '^\\[network\\]' /etc/wsl.conf; then sed -i '/^\\[network\\]/a generateResolvConf = false' /etc/wsl.conf; " +
            "  else printf '\\n[network]\\ngenerateResolvConf = false\\n' >> /etc/wsl.conf; fi; " +
            "else printf '[network]\\ngenerateResolvConf = false\\n' > /etc/wsl.conf; fi;",
            asRoot: true,
            cancellationToken,
            timeout: TimeSpan.FromSeconds(45));

        if (!fix.IsSuccess)
        {
            _logSink.Warn($"WSL DNS fix failed. exit={fix.ExitCode}");
        }
    }

    public async Task ResetWslAsync(CancellationToken cancellationToken, TimeSpan? postShutdownDelay = null)
    {
        var shutdown = await _executor.RunWslAsync("--shutdown", cancellationToken, timeout: TimeSpan.FromSeconds(30));
        if (!shutdown.IsSuccess)
        {
            _logSink.Warn($"WSL shutdown for remediation returned non-zero. exit={shutdown.ExitCode}");
        }

        await Task.Delay(postShutdownDelay ?? TimeSpan.FromSeconds(2), cancellationToken);
    }
}
