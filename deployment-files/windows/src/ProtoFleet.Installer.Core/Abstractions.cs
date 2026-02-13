namespace ProtoFleet.Installer.Core;

public interface IInstallerStep
{
    string Name { get; }

    IReadOnlyCollection<InstallerCheckpoint> ResumeFromCheckpoints => Array.Empty<InstallerCheckpoint>();

    Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface ICommandRunner
{
    Task<CommandResult> RunAsync(CommandRequest request, CancellationToken cancellationToken);
}

public interface ILogSink
{
    void Info(string message);

    void Warn(string message);

    void Error(string message);
}

public interface ISystemPrereqService
{
    Task<HostCheckReport> CheckHostAsync(CancellationToken cancellationToken);
}

public interface IWslSetupService
{
    Task<InstallerStepResult> EnsureReadyAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IDeploymentResolver
{
    Task<DeploymentResolution> ResolveAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IDockerReadinessService
{
    Task<InstallerStepResult> EnsureReadyAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IDeploymentPreparationService
{
    Task<InstallerStepResult> PrepareAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IPluginValidator
{
    Task<InstallerStepResult> ValidateAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IEnvConfigurator
{
    Task<InstallerStepResult> ConfigureAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface INginxConfigurator
{
    Task<InstallerStepResult> ConfigureAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IComposeDeployer
{
    Task<InstallerStepResult> DeployAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IHealthChecker
{
    Task<InstallerStepResult> CheckAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IScheduledTaskService
{
    Task<InstallerStepResult> EnsureTaskAsync(InstallerContext context, CancellationToken cancellationToken);
}

public interface IInstallerResumeStateStore
{
    InstallerResumeState? Load();

    void Save(InstallerResumeState state);

    void Clear();
}

public interface IResumeRegistrationService
{
    void RegisterAutoResume(string executablePath);

    void ClearAutoResume();
}

