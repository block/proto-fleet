namespace ProtoFleet.Installer.Core;

public sealed class CommandResult
{
    public int ExitCode { get; init; }

    public string StandardOutput { get; init; } = string.Empty;

    public string StandardError { get; init; } = string.Empty;

    public TimeSpan Duration { get; init; }

    public bool IsSuccess => ExitCode == 0;
}
