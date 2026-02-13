namespace ProtoFleet.Installer.Core;

public sealed class InstallerWorkflowRunner
{
    private readonly IReadOnlyList<IInstallerStep> _steps;
    private readonly ILogSink _logSink;

    public InstallerWorkflowRunner(IReadOnlyList<IInstallerStep> steps, ILogSink logSink)
    {
        _steps = steps;
        _logSink = logSink;
    }

    public async Task<InstallerRunResult> RunAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        var startIndex = ResolveStartIndex(context);
        if (startIndex > 0)
        {
            _logSink.Info($"Resuming from checkpoint: {context.Checkpoint}");
        }

        for (var stepIndex = startIndex; stepIndex < _steps.Count; stepIndex++)
        {
            var step = _steps[stepIndex];
            cancellationToken.ThrowIfCancellationRequested();
            _logSink.Info($"Starting step: {step.Name}");
            InstallerStepResult result;
            try
            {
                result = await step.ExecuteAsync(context, cancellationToken);
            }
            catch (OperationCanceledException)
            {
                throw;
            }
            catch (Exception ex)
            {
                _logSink.Error($"Unhandled exception in step '{step.Name}': {ex.Message}");
                return new InstallerRunResult
                {
                    ExitCode = InstallerExitCode.Fatal,
                    ErrorMessage = $"Unhandled exception in step '{step.Name}'.",
                    Warnings = context.Warnings,
                    LinuxProvisionedUsername = context.LinuxProvisionedUsername,
                    LinuxCredentialFilePath = context.LinuxCredentialFilePath,
                };
            }

            if (!result.Success)
            {
                if (result.RequiresUserAction)
                {
                    _logSink.Warn($"{step.Name} paused for user action: {result.ErrorMessage}");
                    return new InstallerRunResult
                    {
                        ExitCode = InstallerExitCode.Success,
                        ErrorMessage = result.ErrorMessage,
                        Warnings = context.Warnings,
                        RequiresUserAction = true,
                        UserActionType = result.UserActionType,
                        ActionContext = result.ActionContext,
                        Checkpoint = context.Checkpoint,
                        SelectedDistro = context.SelectedDistro,
                        LinuxProvisionedUsername = context.LinuxProvisionedUsername,
                        LinuxCredentialFilePath = context.LinuxCredentialFilePath,
                    };
                }

                _logSink.Error($"{step.Name} failed (exit code {(int)result.ExitCode}): {result.ErrorMessage}");
                return new InstallerRunResult
                {
                    ExitCode = result.ExitCode,
                    ErrorMessage = result.ErrorMessage,
                    Warnings = context.Warnings,
                    Checkpoint = context.Checkpoint,
                    SelectedDistro = context.SelectedDistro,
                    LinuxProvisionedUsername = context.LinuxProvisionedUsername,
                    LinuxCredentialFilePath = context.LinuxCredentialFilePath,
                };
            }

            _logSink.Info($"Completed step: {step.Name}");
        }

        return new InstallerRunResult
        {
            ExitCode = context.RebootRequired ? InstallerExitCode.RebootRequired : InstallerExitCode.Success,
            Warnings = context.Warnings,
            Checkpoint = context.Checkpoint,
            SelectedDistro = context.SelectedDistro,
            LinuxProvisionedUsername = context.LinuxProvisionedUsername,
            LinuxCredentialFilePath = context.LinuxCredentialFilePath,
        };
    }

    private int ResolveStartIndex(InstallerContext context)
    {
        if (context.Checkpoint == InstallerCheckpoint.None)
        {
            return 0;
        }

        var mappedStep = _steps
            .Select((step, index) => (step, index))
            .FirstOrDefault(x => x.step.ResumeFromCheckpoints.Contains(context.Checkpoint));
        if (mappedStep.step is not null)
        {
            return mappedStep.index;
        }

        _logSink.Warn($"No step metadata matched checkpoint '{context.Checkpoint}'. Starting from first step.");
        return 0;
    }
}
