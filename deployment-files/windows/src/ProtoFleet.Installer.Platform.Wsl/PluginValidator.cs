using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class PluginValidator : IPluginValidator
{
    private static readonly string[] RequiredPlugins =
    [
        "proto-plugin",
        "antminer-plugin"
    ];

    private readonly WslCommandExecutor _executor;
    private readonly ILogSink _logSink;

    public PluginValidator(WslCommandExecutor executor, ILogSink logSink)
    {
        _executor = executor;
        _logSink = logSink;
    }

    public async Task<InstallerStepResult> ValidateAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(context.DeploymentRootWindowsPath) || string.IsNullOrWhiteSpace(context.DeploymentRootWslPath))
        {
            return InstallerStepResult.Failed("Deployment root path was not set.");
        }

        var serverPath = Path.Combine(context.DeploymentRootWindowsPath, "server");
        var missing = RequiredPlugins
            .Where(plugin => !File.Exists(Path.Combine(serverPath, plugin)))
            .ToArray();
        if (missing.Length > 0)
        {
            return InstallerStepResult.Failed($"Required plugin binaries are missing: {string.Join(", ", missing)}.");
        }

        if (!context.DeploymentRootWslPath.StartsWith("/mnt/", StringComparison.OrdinalIgnoreCase))
        {
            var chmodCmd = $"cd {ShellEscaping.BashSingleQuote($"{context.DeploymentRootWslPath}/server")} && chmod +x {string.Join(' ', RequiredPlugins.Select(ShellEscaping.BashSingleQuote))}";
            var chmod = await _executor.RunInDistroAsync(context.SelectedDistro!, chmodCmd, asRoot: true, cancellationToken);
            if (!chmod.IsSuccess)
            {
                _logSink.Warn("Could not apply chmod +x to plugin binaries.");
            }
        }

        return InstallerStepResult.Succeeded();
    }
}
