using System.Formats.Tar;
using System.IO.Compression;
using System.Text;
using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Core.Services;
using ProtoFleet.Installer.Platform.Wsl;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class DeploymentPreparationServiceTests : IDisposable
{
    private const string ForkRepository = "example-owner/fleet-fork";
    private readonly string _root = Path.Combine(Path.GetTempPath(), "protofleet-prepare-tests", Guid.NewGuid().ToString("N"));

    public DeploymentPreparationServiceTests()
    {
        Directory.CreateDirectory(_root);
    }

    [Theory]
    [InlineData(ForkRepository, "fresh\r\n")]
    [InlineData(ForkRepository, "fresh\n")]
    [InlineData("block/proto-fleet", "fresh\r\n")]
    [InlineData(ForkRepository, "installed\r\nversion: v1.0.0\r\nrelease_repository: example-owner/fleet-fork\r\ncommit: abc123\r\n")]
    [InlineData(ForkRepository, "installed\nrelease_repository: example-owner/fleet-fork\n")]
    [InlineData("block/proto-fleet", "installed\r\nrelease_repository: block/proto-fleet\r\n")]
    [InlineData("block/proto-fleet", "installed\r\nversion: v1.0.0\r\n")]
    [InlineData("block/proto-fleet", "installed\r\n")]
    [InlineData("block/proto-fleet", "installed\r\n__fresh_install__\r\n")]
    public async Task AcceptsFreshOrMatchingSourceBeforeExtraction(string repository, string installedMetadata)
    {
        var context = Context(repository);
        var runner = new StubCommandRunner(installedMetadata, $"PROTO_FLEET_RELEASE_REPOSITORY={repository}\r\n");

        var result = await Service(runner).PrepareAsync(context, CancellationToken.None);

        // Stop at extraction so this boundary test needs neither WSL nor a UNC share.
        Assert.False(result.Success);
        Assert.Equal("Failed to extract tarball in WSL.", result.ErrorMessage);
        Assert.Equal(4, runner.Requests.Count);
        Assert.Contains("tar -xzf", runner.Requests[^1].Arguments);
    }

    [Theory]
    [InlineData("installed\r\nversion: v1.0.0\r\n")]
    [InlineData("installed\r\nrelease_repository: block/proto-fleet\r\n")]
    [InlineData("installed\r\nrelease_repository: other-owner/fleet-fork\r\n")]
    [InlineData("installed\r\n__fresh_install__\r\n")]
    [InlineData("installed\n__fresh_install__")]
    [InlineData("installed\r\nfresh\r\n")]
    [InlineData("installed\r\n")]
    public async Task RejectsDifferentInstalledRepositoryBeforeExtraction(string installedMetadata)
    {
        var runner = new StubCommandRunner(installedMetadata);

        var result = await Service(runner).PrepareAsync(Context(ForkRepository), CancellationToken.None);

        Assert.False(result.Success);
        Assert.Contains("Release repository differs", result.ErrorMessage);
        Assert.Equal(2, runner.Requests.Count);
        Assert.DoesNotContain(runner.Requests, request => request.Arguments.Contains("tar -xzf"));
    }

    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task RejectsConflictingEnvironmentBeforeExtraction(bool suppliedConfig)
    {
        const string conflictingEnvironment = "PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\r\n";
        var runner = new StubCommandRunner("fresh\r\n", suppliedConfig ? "" : conflictingEnvironment);
        var configPath = Path.Combine(_root, "provided.env");
        File.WriteAllText(configPath, conflictingEnvironment);
        var context = Context(ForkRepository, suppliedConfig ? configPath : null);

        await Assert.ThrowsAsync<InvalidDataException>(() => Service(runner).PrepareAsync(context, CancellationToken.None));

        Assert.Equal(3, runner.Requests.Count);
        Assert.DoesNotContain(runner.Requests, request => request.Arguments.Contains("tar -xzf"));
    }

    [Theory]
    [InlineData("")]
    [InlineData("__fresh_install__\r\n")]
    [InlineData("fresh\r\nunexpected metadata\r\n")]
    [InlineData("unknown\r\n")]
    [InlineData("installed")]
    public async Task RejectsInvalidInstallationStateBeforeExtraction(string output)
    {
        var runner = new StubCommandRunner(output);

        var result = await Service(runner).PrepareAsync(Context(ForkRepository), CancellationToken.None);

        Assert.False(result.Success);
        Assert.Contains("Invalid installation state", result.ErrorMessage);
        Assert.Equal(2, runner.Requests.Count);
        Assert.DoesNotContain(runner.Requests, request => request.Arguments.Contains("tar -xzf"));
    }

    [Fact]
    public async Task RejectsCrlfArchiveMetadataBeforeExtraction()
    {
        var runner = new StubCommandRunner("fresh\r\n");
        var context = Context(ForkRepository, lineEnding: "\r\n");

        await Assert.ThrowsAsync<InvalidDataException>(() => Service(runner).PrepareAsync(context, CancellationToken.None));

        Assert.Single(runner.Requests);
        Assert.DoesNotContain(runner.Requests, request => request.Arguments.Contains("tar -xzf"));
    }

    [Theory]
    [InlineData("fresh\r\n", true)]
    [InlineData("installed\r\nversion: v1.0.0\r\n", true)]
    [InlineData("installed\r\nrelease_repository: example-owner/fleet-fork\r\n", false)]
    public async Task MetadataFreeDeploymentDirectoryUsesOfficialSource(string installedMetadata, bool reachesExtraction)
    {
        var context = await DirectoryContext();
        var runner = new StubCommandRunner(installedMetadata);

        var result = await Service(runner).PrepareAsync(context, CancellationToken.None);

        Assert.False(result.Success);
        if (reachesExtraction)
        {
            Assert.Equal("Failed to extract tarball in WSL.", result.ErrorMessage);
            Assert.Equal(4, runner.Requests.Count);
            Assert.Contains("tar -xzf", runner.Requests[^1].Arguments);
        }
        else
        {
            Assert.Contains("Release repository differs", result.ErrorMessage);
            Assert.Equal(2, runner.Requests.Count);
            Assert.DoesNotContain(runner.Requests, request => request.Arguments.Contains("tar -xzf"));
        }
        Assert.False(File.Exists(Path.Combine(context.DeploymentRootWindowsPath!, "version.txt")));
    }

    [Fact]
    public async Task DirectoryMetadataIsStillValidatedBeforeExtraction()
    {
        var context = await DirectoryContext();
        File.WriteAllText(Path.Combine(context.DeploymentRootWindowsPath!, "version.txt"),
            $"release_repository: {ForkRepository}\r\n");
        var runner = new StubCommandRunner("fresh\r\n");

        var result = await Service(runner).PrepareAsync(context, CancellationToken.None);

        Assert.False(result.Success);
        Assert.Contains("Release repository must be", result.ErrorMessage);
        Assert.Single(runner.Requests);
        Assert.DoesNotContain(runner.Requests, request => request.Arguments.Contains("tar -xzf"));
    }

    private async Task<InstallerContext> DirectoryContext()
    {
        var directory = Path.Combine(_root, "deployment");
        Directory.CreateDirectory(Path.Combine(directory, "server"));
        Directory.CreateDirectory(Path.Combine(directory, "client"));
        File.WriteAllText(Path.Combine(directory, "docker-compose.yaml"), "services: {}\n");
        var context = new InstallerContext
        {
            Options = new InstallerOptions { DeploymentPath = directory },
            SelectedDistro = "test-distro"
        };
        var resolution = await new DeploymentResolver().ResolveAsync(context, CancellationToken.None);
        Assert.True(resolution.IsResolved);
        context.DeploymentRootWindowsPath = resolution.DeploymentRootPath;
        return context;
    }

    private InstallerContext Context(string repository, string? configPath = null, string lineEnding = "\n")
    {
        var path = Path.Combine(_root, "release.tar.gz");
        using (var file = File.Create(path))
        using (var gzip = new GZipStream(file, CompressionMode.Compress))
        using (var writer = new TarWriter(gzip))
        using (var metadata = new MemoryStream(Encoding.UTF8.GetBytes($"release_repository: {repository}{lineEnding}")))
        {
            writer.WriteEntry(new PaxTarEntry(TarEntryType.RegularFile, "deployment/version.txt") { DataStream = metadata });
        }
        return new InstallerContext
        {
            Options = new InstallerOptions { ConfigFilePath = configPath },
            SelectedDistro = "test-distro",
            TarballPath = path
        };
    }

    private static DeploymentPreparationService Service(StubCommandRunner runner)
    {
        var log = new NoOpLogSink();
        return new DeploymentPreparationService(new WslCommandExecutor(runner, log), log);
    }

    public void Dispose() => Directory.Delete(_root, recursive: true);

    private sealed class StubCommandRunner(string metadata, string environment = "") : ICommandRunner
    {
        public List<CommandRequest> Requests { get; } = new();

        public Task<CommandResult> RunAsync(CommandRequest request, CancellationToken cancellationToken)
        {
            Requests.Add(request);
            var result = Requests.Count switch
            {
                1 => new CommandResult { StandardOutput = "/tmp/release.tar.gz\r\n" },
                2 => new CommandResult { StandardOutput = metadata },
                3 => new CommandResult { StandardOutput = environment },
                4 => new CommandResult { ExitCode = 1 },
                _ => throw new InvalidOperationException("Unexpected WSL command")
            };
            return Task.FromResult(result);
        }
    }

    private sealed class NoOpLogSink : ILogSink
    {
        public void Info(string message) { }
        public void Warn(string message) { }
        public void Error(string message) { }
    }
}
