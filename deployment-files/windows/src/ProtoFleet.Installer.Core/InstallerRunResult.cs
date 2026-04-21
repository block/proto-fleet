namespace ProtoFleet.Installer.Core;

public sealed class InstallerRunResult
{
    public required InstallerExitCode ExitCode { get; init; }

    public string? ErrorMessage { get; init; }

    public IReadOnlyList<string> Warnings { get; init; } = Array.Empty<string>();

    public bool RequiresUserAction { get; init; }

    public InstallerUserActionType UserActionType { get; init; } = InstallerUserActionType.None;

    public string? ActionContext { get; init; }

    public InstallerCheckpoint Checkpoint { get; init; } = InstallerCheckpoint.None;

    public string? SelectedDistro { get; init; }

    public string? LinuxProvisionedUsername { get; init; }

    public string? LinuxCredentialFilePath { get; init; }
}
