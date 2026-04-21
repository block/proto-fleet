namespace ProtoFleet.Installer.Core.Steps;

public sealed class ConfigureNginxStep : IInstallerStep
{
    private readonly INginxConfigurator _nginxConfigurator;

    public ConfigureNginxStep(INginxConfigurator nginxConfigurator)
    {
        _nginxConfigurator = nginxConfigurator;
    }

    public string Name => "Configure nginx profile";

    public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return _nginxConfigurator.ConfigureAsync(context, cancellationToken);
    }
}
