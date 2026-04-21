namespace ProtoFleet.Installer.Core.Steps;

public sealed class ValidatePluginsStep : IInstallerStep
{
    private readonly IPluginValidator _pluginValidator;

    public ValidatePluginsStep(IPluginValidator pluginValidator)
    {
        _pluginValidator = pluginValidator;
    }

    public string Name => "Validate plugin binaries";

    public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return _pluginValidator.ValidateAsync(context, cancellationToken);
    }
}
