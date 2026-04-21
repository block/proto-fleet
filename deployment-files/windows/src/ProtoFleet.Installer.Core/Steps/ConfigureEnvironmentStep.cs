namespace ProtoFleet.Installer.Core.Steps;

public sealed class ConfigureEnvironmentStep : IInstallerStep
{
    private readonly IEnvConfigurator _envConfigurator;

    public ConfigureEnvironmentStep(IEnvConfigurator envConfigurator)
    {
        _envConfigurator = envConfigurator;
    }

    public string Name => "Configure .env";

    public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return _envConfigurator.ConfigureAsync(context, cancellationToken);
    }
}
