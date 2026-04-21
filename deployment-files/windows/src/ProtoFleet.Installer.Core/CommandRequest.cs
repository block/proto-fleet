using System.Text;

namespace ProtoFleet.Installer.Core;

public sealed class CommandRequest
{
    public required string FileName { get; init; }

    public required string Arguments { get; init; }

    public string? WorkingDirectory { get; init; }

    public IDictionary<string, string>? EnvironmentVariables { get; init; }

    public TimeSpan Timeout { get; init; } = TimeSpan.FromMinutes(5);

    public Encoding? StandardOutputEncoding { get; init; }

    public Encoding? StandardErrorEncoding { get; init; }
}
