using FluentAssertions;
using ProtoFleet.Installer.Core;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class InstallerWorkflowRunnerCancellationTests
{
    [Fact]
    public async Task RunAsync_ThrowsOperationCanceledWhenTokenIsCancelled()
    {
        var steps = new IInstallerStep[]
        {
            new BlockingStep()
        };
        var runner = new InstallerWorkflowRunner(steps, new NoOpLogSink());
        var context = new InstallerContext
        {
            Options = new InstallerOptions()
        };
        using var cts = new CancellationTokenSource();
        cts.CancelAfter(TimeSpan.FromMilliseconds(50));

        var run = () => runner.RunAsync(context, cts.Token);

        await run.Should().ThrowAsync<OperationCanceledException>();
    }

    private sealed class BlockingStep : IInstallerStep
    {
        public string Name => "Blocking";

        public async Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
        {
            await Task.Delay(TimeSpan.FromSeconds(10), cancellationToken);
            return InstallerStepResult.Succeeded();
        }
    }

    private sealed class NoOpLogSink : ILogSink
    {
        public void Info(string message) { }

        public void Warn(string message) { }

        public void Error(string message) { }
    }
}
