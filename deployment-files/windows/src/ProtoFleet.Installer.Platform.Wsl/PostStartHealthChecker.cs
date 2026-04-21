using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class PostStartHealthChecker : IHealthChecker
{
    private readonly WslCommandExecutor _executor;
    private readonly ILogSink _logSink;

    public PostStartHealthChecker(WslCommandExecutor executor, ILogSink logSink)
    {
        _executor = executor;
        _logSink = logSink;
    }

    public async Task<InstallerStepResult> CheckAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(context.SelectedDistro) || string.IsNullOrWhiteSpace(context.DeploymentRootWslPath))
        {
            return InstallerStepResult.Failed("Health check prerequisites were missing.");
        }

        var deadline = DateTimeOffset.UtcNow.AddSeconds(60);
        while (DateTimeOffset.UtcNow <= deadline)
        {
            var result = await _executor.RunInDistroAsync(
                context.SelectedDistro,
                $"cd {ShellEscaping.BashSingleQuote(context.DeploymentRootWslPath)} && docker ps --format '{{{{.Names}}}}'",
                asRoot: true,
                cancellationToken);

            if (result.IsSuccess && !string.IsNullOrWhiteSpace(result.StandardOutput))
            {
                _logSink.Info("Running service containers detected.");
                return InstallerStepResult.Succeeded();
            }

            await Task.Delay(TimeSpan.FromSeconds(3), cancellationToken);
        }

        context.Warnings.Add("Service health check timed out after 60 seconds. Installer continued.");
        _logSink.Warn("Service health check timed out after 60 seconds.");
        return InstallerStepResult.Succeeded();
    }
}
