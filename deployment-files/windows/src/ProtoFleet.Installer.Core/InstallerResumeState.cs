namespace ProtoFleet.Installer.Core;

public sealed class InstallerResumeState
{
    public required InstallerOptions Options { get; init; }

    public InstallerCheckpoint Checkpoint { get; init; } = InstallerCheckpoint.None;

    public string? SelectedDistro { get; init; }

    public DateTimeOffset CreatedAtUtc { get; init; } = DateTimeOffset.UtcNow;
}
