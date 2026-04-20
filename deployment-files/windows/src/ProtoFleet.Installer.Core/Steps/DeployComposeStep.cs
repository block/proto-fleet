namespace ProtoFleet.Installer.Core.Steps;

public sealed class DeployComposeStep : IInstallerStep
{
    private readonly IComposeDeployer _composeDeployer;

    public DeployComposeStep(IComposeDeployer composeDeployer)
    {
        _composeDeployer = composeDeployer;
    }

    public string Name => "Deploy docker compose stack";

    public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return _composeDeployer.DeployAsync(context, cancellationToken);
    }
}
