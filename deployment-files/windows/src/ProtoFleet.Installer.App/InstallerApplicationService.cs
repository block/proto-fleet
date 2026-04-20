using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Core.Services;
using ProtoFleet.Installer.Core.Steps;
using ProtoFleet.Installer.Platform.Windows;
using ProtoFleet.Installer.Platform.Wsl;

namespace ProtoFleet.Installer.App;

public sealed class InstallerApplicationService
{
    private readonly Action<string> _onLogLine;
    private readonly Func<string> _resolveLogPath;

    public InstallerApplicationService(Action<string> onLogLine, Func<string> resolveLogPath)
    {
        _onLogLine = onLogLine;
        _resolveLogPath = resolveLogPath;
    }

    public ILogSink? ActiveLogSink { get; private set; }

    public Task<InstallerRunResult> RunAsync(InstallerOptions options, CancellationToken cancellationToken)
    {
        return RunInternalAsync(options, resumeDistro: null, cancellationToken);
    }

    public Task<InstallerRunResult> ResumeFromLinuxUserSetupAsync(
        InstallerOptions options,
        string distro,
        CancellationToken cancellationToken)
    {
        return RunInternalAsync(options, distro, cancellationToken);
    }

    public WslCommandExecutor CreateWslExecutor()
    {
        var logSink = ActiveLogSink ?? new UiLogSink(_onLogLine);
        return new WslCommandExecutor(new ProcessCommandRunner(), logSink);
    }

    private async Task<InstallerRunResult> RunInternalAsync(
        InstallerOptions options,
        string? resumeDistro,
        CancellationToken cancellationToken)
    {
        var uiLogSink = new UiLogSink(_onLogLine);
        var fileLogSink = new FileLogSink(_resolveLogPath());
        var logSink = new CompositeLogSink(uiLogSink, fileLogSink);
        ActiveLogSink = logSink;

        var commandRunner = new ProcessCommandRunner();
        var wslExecutor = new WslCommandExecutor(commandRunner, logSink);
        var dockerReadiness = new DockerReadinessService(wslExecutor, logSink);
        var steps = BuildSteps(commandRunner, wslExecutor, dockerReadiness, logSink);
        var workflowRunner = new InstallerWorkflowRunner(steps, logSink);
        var orchestrator = new InstallerOrchestrator(workflowRunner);

        if (string.IsNullOrWhiteSpace(resumeDistro))
        {
            return await orchestrator.RunAsync(options, cancellationToken);
        }

        var resumedContext = new InstallerContext
        {
            Options = options,
            Checkpoint = InstallerCheckpoint.LinuxUserProvisioning,
            SelectedDistro = resumeDistro
        };
        return await orchestrator.RunAsync(resumedContext, cancellationToken);
    }

    private static IReadOnlyList<IInstallerStep> BuildSteps(
        ICommandRunner commandRunner,
        WslCommandExecutor wslExecutor,
        IDockerReadinessService dockerReadiness,
        ILogSink logSink)
    {
        return new IInstallerStep[]
        {
            new PreflightAndSetupStep(
                new SystemPrereqService(),
                new WslSetupService(wslExecutor, dockerReadiness, logSink),
                logSink),
            new ResolveDeploymentStep(
                new DeploymentResolver(),
                new DeploymentPreparationService(wslExecutor, logSink),
                logSink),
            new ValidatePluginsStep(new PluginValidator(wslExecutor, logSink)),
            new ConfigureEnvironmentStep(new EnvConfigurator(logSink)),
            new ConfigureNginxStep(new NginxConfigurator(logSink)),
            new DeployComposeStep(new ComposeDeployer(wslExecutor, dockerReadiness, logSink)),
            new PostStartHealthStep(new PostStartHealthChecker(wslExecutor, logSink)),
            new ScheduledTaskStep(new ScheduledTaskService(commandRunner, logSink))
        };
    }
}
