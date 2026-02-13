namespace ProtoFleet.Installer.Core;

public sealed class HostCheckReport
{
    public bool IsSupportedBuild { get; init; }

    public int WindowsBuild { get; init; }

    public ulong TotalMemoryBytes { get; init; }

    public long CDriveFreeBytes { get; init; }

    public IReadOnlyList<WindowsFeatureCheck> WindowsFeatureChecks { get; init; } = Array.Empty<WindowsFeatureCheck>();

    public IReadOnlyList<string> Warnings { get; init; } = Array.Empty<string>();

    public bool RequiresReboot { get; init; }

    public string? RebootMessage { get; init; }

    public string? FatalError { get; init; }
}
