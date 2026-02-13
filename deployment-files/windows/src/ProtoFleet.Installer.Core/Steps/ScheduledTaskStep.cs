namespace ProtoFleet.Installer.Core.Steps;

public sealed class ScheduledTaskStep : IInstallerStep
{
    private readonly IScheduledTaskService _scheduledTaskService;

    public ScheduledTaskStep(IScheduledTaskService scheduledTaskService)
    {
        _scheduledTaskService = scheduledTaskService;
    }

    public string Name => "Configure WSL auto-start task";

    public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (!context.Options.EnableAutoStartTask)
        {
            return Task.FromResult(InstallerStepResult.Succeeded());
        }

        return _scheduledTaskService.EnsureTaskAsync(context, cancellationToken);
    }
}
