namespace ProtoFleet.Installer.Core;

public sealed class InstallerContext
{
    public required InstallerOptions Options { get; init; }

    public string? SelectedDistro { get; set; }

    public string? DeploymentRootWindowsPath { get; set; }

    public string? DeploymentRootWslPath { get; set; }

    public string? TarballPath { get; set; }

    public ProtocolMode EffectiveProtocolMode { get; set; }

    public bool RebootRequired { get; set; }

    public InstallerCheckpoint Checkpoint { get; set; } = InstallerCheckpoint.None;

    public bool LinuxUserVerified { get; set; }

    public DateTimeOffset? LinuxUserVerifiedAt { get; set; }

    public string? DeploymentTransferMode { get; set; }

    public string? LinuxProvisionedUsername { get; set; }

    public string? LinuxCredentialFilePath { get; set; }

    public List<string> Warnings { get; } = new();
}
