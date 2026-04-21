namespace ProtoFleet.Installer.Core;

public sealed class DeploymentResolution
{
    public required bool IsResolved { get; init; }

    public string? DeploymentRootPath { get; init; }

    public string? TarballPath { get; init; }

    public string? FailureReason { get; init; }
}
