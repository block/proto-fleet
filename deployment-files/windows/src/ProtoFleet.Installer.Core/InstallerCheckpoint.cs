namespace ProtoFleet.Installer.Core;

public enum InstallerCheckpoint
{
    None = 0,
    WslSetup = 1,
    LinuxUserProvisioning = 2,
    DockerSetup = 3,
    DeploymentPreparation = 4
}
