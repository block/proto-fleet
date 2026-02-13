namespace ProtoFleet.Installer.Core;

public enum InstallerExitCode
{
    Success = 0,
    Fatal = 1,
    RebootRequired = 2,
    InvalidDeploymentInput = 3,
    Cancelled = 4
}
