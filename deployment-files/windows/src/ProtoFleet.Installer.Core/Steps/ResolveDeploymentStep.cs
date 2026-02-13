namespace ProtoFleet.Installer.Core.Steps;

public sealed class ResolveDeploymentStep : IInstallerStep
{
    private static readonly IReadOnlyCollection<InstallerCheckpoint> ResumeCheckpoints =
    [
        InstallerCheckpoint.DeploymentPreparation
    ];

    private readonly IDeploymentResolver _deploymentResolver;
    private readonly IDeploymentPreparationService _deploymentPreparationService;
    private readonly ILogSink _logSink;

    public ResolveDeploymentStep(
        IDeploymentResolver deploymentResolver,
        IDeploymentPreparationService deploymentPreparationService,
        ILogSink logSink)
    {
        _deploymentResolver = deploymentResolver;
        _deploymentPreparationService = deploymentPreparationService;
        _logSink = logSink;
    }

    public string Name => "Resolve deployment source";

    public IReadOnlyCollection<InstallerCheckpoint> ResumeFromCheckpoints => ResumeCheckpoints;

    public async Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        var resolution = await _deploymentResolver.ResolveAsync(context, cancellationToken);
        if (!resolution.IsResolved)
        {
            return InstallerStepResult.Failed(
                resolution.FailureReason ?? "Failed to resolve deployment path or tarball input.",
                InstallerExitCode.InvalidDeploymentInput);
        }

        context.DeploymentRootWindowsPath = resolution.DeploymentRootPath;
        context.TarballPath = resolution.TarballPath;

        _logSink.Info($"Deployment root (Windows): {context.DeploymentRootWindowsPath ?? "<from tarball>"}");
        if (!string.IsNullOrWhiteSpace(context.TarballPath))
        {
            _logSink.Info($"Tarball: {context.TarballPath}");
        }

        return await _deploymentPreparationService.PrepareAsync(context, cancellationToken);
    }
}
