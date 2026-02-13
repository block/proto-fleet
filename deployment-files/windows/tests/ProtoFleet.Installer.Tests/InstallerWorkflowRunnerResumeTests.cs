using FluentAssertions;
using ProtoFleet.Installer.Core;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class InstallerWorkflowRunnerResumeTests
{
    [Fact]
    public async Task RunAsync_StartsFromStepMatchingCheckpointMetadata()
    {
        var executed = new List<string>();
        var steps = new IInstallerStep[]
        {
            new FakeStep("A", Array.Empty<InstallerCheckpoint>(), executed),
            new FakeStep("B", new[] { InstallerCheckpoint.DeploymentPreparation }, executed),
            new FakeStep("C", Array.Empty<InstallerCheckpoint>(), executed),
        };
        var runner = new InstallerWorkflowRunner(steps, new TestLogSink());

        var context = new InstallerContext
        {
            Options = new InstallerOptions(),
            Checkpoint = InstallerCheckpoint.DeploymentPreparation
        };

        var result = await runner.RunAsync(context, CancellationToken.None);

        result.ExitCode.Should().Be(InstallerExitCode.Success);
        executed.Should().ContainInOrder("B", "C");
        executed.Should().NotContain("A");
    }

    [Fact]
    public async Task RunAsync_FallsBackToFirstStepWhenCheckpointIsUnmapped()
    {
        var executed = new List<string>();
        var steps = new IInstallerStep[]
        {
            new FakeStep("A", Array.Empty<InstallerCheckpoint>(), executed),
            new FakeStep("B", Array.Empty<InstallerCheckpoint>(), executed),
        };
        var runner = new InstallerWorkflowRunner(steps, new TestLogSink());

        var context = new InstallerContext
        {
            Options = new InstallerOptions(),
            Checkpoint = InstallerCheckpoint.WslSetup
        };

        var result = await runner.RunAsync(context, CancellationToken.None);

        result.ExitCode.Should().Be(InstallerExitCode.Success);
        executed.Should().ContainInOrder("A", "B");
    }

    private sealed class FakeStep : IInstallerStep
    {
        private readonly List<string> _executed;

        public FakeStep(string name, IReadOnlyCollection<InstallerCheckpoint> checkpoints, List<string> executed)
        {
            Name = name;
            ResumeFromCheckpoints = checkpoints;
            _executed = executed;
        }

        public string Name { get; }

        public IReadOnlyCollection<InstallerCheckpoint> ResumeFromCheckpoints { get; }

        public Task<InstallerStepResult> ExecuteAsync(InstallerContext context, CancellationToken cancellationToken)
        {
            _executed.Add(Name);
            return Task.FromResult(InstallerStepResult.Succeeded());
        }
    }

    private sealed class TestLogSink : ILogSink
    {
        public void Info(string message) { }

        public void Warn(string message) { }

        public void Error(string message) { }
    }
}
