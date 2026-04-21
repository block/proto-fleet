namespace ProtoFleet.Installer.Core;

public sealed record class InstallerOptions
{
    public string VersionLabel { get; init; } = "latest";

    public string? DeploymentPath { get; init; }

    public string? TarballPath { get; init; }

    public string? ConfigFilePath { get; init; }

    public string InstallDir { get; init; } = "~/proto-fleet";

    public SetupMode SetupMode { get; init; } = SetupMode.Simple;

    public ProtocolMode ProtocolMode { get; init; } = ProtocolMode.Auto;

    public string? ExistingCertPath { get; init; }

    public string? ExistingKeyPath { get; init; }

    public bool EnableAutoStartTask { get; init; }

    public bool AutoStart { get; init; }

    public bool Debug { get; init; }
}
