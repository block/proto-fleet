using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.App;

public sealed class UiLogSink : ILogSink
{
    private readonly Action<string> _onLine;

    public UiLogSink(Action<string> onLine)
    {
        _onLine = onLine;
    }

    public void Info(string message) => _onLine($"[{DateTime.Now:HH:mm:ss}] [INFO] {message}");

    public void Warn(string message) => _onLine($"[{DateTime.Now:HH:mm:ss}] [WARN] {message}");

    public void Error(string message) => _onLine($"[{DateTime.Now:HH:mm:ss}] [ERROR] {message}");
}
