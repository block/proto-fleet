using System.Text;

namespace ProtoFleet.Installer.Core;

public sealed class CompositeLogSink : ILogSink
{
    private readonly IReadOnlyList<ILogSink> _sinks;

    public CompositeLogSink(params ILogSink[] sinks)
    {
        _sinks = sinks;
    }

    public void Info(string message)
    {
        foreach (var sink in _sinks)
        {
            sink.Info(message);
        }
    }

    public void Warn(string message)
    {
        foreach (var sink in _sinks)
        {
            sink.Warn(message);
        }
    }

    public void Error(string message)
    {
        foreach (var sink in _sinks)
        {
            sink.Error(message);
        }
    }
}

public sealed class FileLogSink : ILogSink
{
    private readonly object _sync = new();
    private readonly string _logPath;

    public FileLogSink(string logPath)
    {
        _logPath = logPath;
        try
        {
            var parent = Path.GetDirectoryName(logPath);
            if (!string.IsNullOrWhiteSpace(parent))
            {
                Directory.CreateDirectory(parent);
            }
        }
        catch
        {
            // Best effort only; writes are guarded too.
        }
    }

    public void Info(string message) => Write("INFO", message);

    public void Warn(string message) => Write("WARN", message);

    public void Error(string message) => Write("ERROR", message);

    private void Write(string level, string message)
    {
        try
        {
            lock (_sync)
            {
                File.AppendAllText(_logPath, $"{DateTimeOffset.Now:O} [{level}] {message}{Environment.NewLine}", Encoding.UTF8);
            }
        }
        catch
        {
            // Best effort logging should not fail installation.
        }
    }
}
