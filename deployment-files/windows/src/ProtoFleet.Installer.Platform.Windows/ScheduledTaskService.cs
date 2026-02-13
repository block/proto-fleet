using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Windows;

public sealed class ScheduledTaskService : IScheduledTaskService
{
    private const string TaskName = "ProtoFleet-StartWSL";
    private readonly ICommandRunner _commandRunner;
    private readonly ILogSink _logSink;

    public ScheduledTaskService(ICommandRunner commandRunner, ILogSink logSink)
    {
        _commandRunner = commandRunner;
        _logSink = logSink;
    }

    public async Task<InstallerStepResult> EnsureTaskAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(context.SelectedDistro))
        {
            return InstallerStepResult.Failed("Selected distro is required for scheduled task configuration.");
        }

        var query = await _commandRunner.RunAsync(new CommandRequest
        {
            FileName = "schtasks.exe",
            Arguments = $"/Query /TN \"{TaskName}\""
        }, cancellationToken);

        if (!query.IsSuccess)
        {
            var action = ScheduledTaskCommandBuilder.BuildTaskAction(context.SelectedDistro);
            var createArgs =
                "/Create /F /SC ONLOGON /DELAY 0000:10 /RL HIGHEST " +
                $"/TN \"{TaskName}\" " +
                $"/TR {CommandEscaping.WindowsArgument(action)}";
            var create = await _commandRunner.RunAsync(new CommandRequest
            {
                FileName = "schtasks.exe",
                Arguments = createArgs
            }, cancellationToken);

            if (!create.IsSuccess)
            {
                _logSink.Warn("Could not create scheduled task automatically.");
                _logSink.Warn("Manual command: schtasks /Create /SC ONLOGON /DELAY 0000:10 /RL HIGHEST /TN ProtoFleet-StartWSL /TR \"wsl.exe -d <distro> -u root -- bash -lc 'systemctl start docker || service docker start'\"");
                return InstallerStepResult.Succeeded();
            }
        }

        return InstallerStepResult.Succeeded();
    }
}
