namespace ProtoFleet.Installer.Core.Steps;

public sealed class PreflightAndSetupStep : IInstallerStep
{
    private static readonly IReadOnlyCollection<InstallerCheckpoint> ResumeCheckpoints =
    [
        InstallerCheckpoint.WslSetup,
        InstallerCheckpoint.LinuxUserProvisioning,
        InstallerCheckpoint.DockerSetup
    ];

    private readonly ISystemPrereqService _systemPrereqService;
    private readonly IWslSetupService _wslSetupService;
    private readonly ILogSink _logSink;

    public PreflightAndSetupStep(
        ISystemPrereqService systemPrereqService,
        IWslSetupService wslSetupService,
        ILogSink logSink)
    {
        _systemPrereqService = systemPrereqService;
        _wslSetupService = wslSetupService;
        _logSink = logSink;
    }

    public string Name => "Host preflight + WSL/Docker setup";

    public IReadOnlyCollection<InstallerCheckpoint> ResumeFromCheckpoints => ResumeCheckpoints;

    public async Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        var host = await _systemPrereqService.CheckHostAsync(cancellationToken);

        foreach (var feature in host.WindowsFeatureChecks)
        {
            if (feature.IsBlocking)
            {
                _logSink.Warn(
                    $"Windows feature '{feature.Name}' state is '{feature.State}'. {feature.RemediationMessage}");
            }
            else
            {
                _logSink.Info($"Windows feature '{feature.Name}' state is '{feature.State}'.");
            }
        }

        foreach (var warning in host.Warnings)
        {
            context.Warnings.Add(warning);
            _logSink.Warn(warning);
        }

        if (!string.IsNullOrWhiteSpace(host.FatalError))
        {
            return InstallerStepResult.Failed(host.FatalError);
        }

        if (host.RequiresReboot)
        {
            context.RebootRequired = true;
            var rebootMessage = string.IsNullOrWhiteSpace(host.RebootMessage)
                ? "A reboot is required before installation can continue."
                : host.RebootMessage;
            return InstallerStepResult.Failed(rebootMessage, InstallerExitCode.RebootRequired);
        }

        var setupResult = await _wslSetupService.EnsureReadyAsync(context, cancellationToken);
        if (!setupResult.Success && setupResult.ExitCode == InstallerExitCode.RebootRequired)
        {
            context.RebootRequired = true;
        }

        return setupResult;
    }
}
