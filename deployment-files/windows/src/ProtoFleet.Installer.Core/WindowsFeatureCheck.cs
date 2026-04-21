namespace ProtoFleet.Installer.Core;

public sealed class WindowsFeatureCheck
{
    public string Name { get; init; } = string.Empty;

    public string State { get; init; } = string.Empty;

    public bool IsBlocking { get; init; }

    public string? RemediationMessage { get; init; }
}
