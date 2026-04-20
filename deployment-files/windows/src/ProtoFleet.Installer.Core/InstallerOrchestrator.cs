namespace ProtoFleet.Installer.Core;

public sealed class InstallerOrchestrator
{
    private readonly InstallerWorkflowRunner _workflowRunner;

    public InstallerOrchestrator(InstallerWorkflowRunner workflowRunner)
    {
        _workflowRunner = workflowRunner;
    }

    public Task<InstallerRunResult> RunAsync(InstallerOptions options, CancellationToken cancellationToken)
    {
        var context = new InstallerContext
        {
            Options = options
        };

        return _workflowRunner.RunAsync(context, cancellationToken);
    }

    public Task<InstallerRunResult> RunAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return _workflowRunner.RunAsync(context, cancellationToken);
    }
}
