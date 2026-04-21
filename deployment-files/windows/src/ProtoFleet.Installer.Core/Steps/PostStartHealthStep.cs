namespace ProtoFleet.Installer.Core.Steps;

public sealed class PostStartHealthStep : IInstallerStep
{
    private readonly IHealthChecker _healthChecker;

    public PostStartHealthStep(IHealthChecker healthChecker)
    {
        _healthChecker = healthChecker;
    }

    public string Name => "Post-start health check";

    public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return _healthChecker.CheckAsync(context, cancellationToken);
    }
}
