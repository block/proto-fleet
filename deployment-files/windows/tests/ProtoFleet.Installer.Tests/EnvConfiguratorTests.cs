using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Core.Services;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class EnvConfiguratorTests : IDisposable
{
    private const string ForkRepository = "example-owner/fleet-fork";
    private readonly string _root = Path.Combine(Path.GetTempPath(), "protofleet-env-tests", Guid.NewGuid().ToString("N"));

    public EnvConfiguratorTests()
    {
        Directory.CreateDirectory(_root);
    }

    [Theory]
    [InlineData("version: v1.0.0\r\n", "block/proto-fleet")]
    [InlineData("release_repository: example-owner/fleet-fork\n", ForkRepository)]
    public async Task FreshConfigurationPersistsBundleRepository(string metadata, string repository)
    {
        File.WriteAllText(Path.Combine(_root, "version.txt"), metadata);

        var result = await new EnvConfigurator(new NoOpLogSink()).ConfigureAsync(Context(), CancellationToken.None);

        Assert.True(result.Success, result.ErrorMessage);
        var values = EnvFile.Parse(Path.Combine(_root, ".env"));
        Assert.Equal(repository, values[ReleaseSource.EnvironmentKey]);
        Assert.True(EnvFile.HasRequiredKeys(values, out _));
        Assert.True(EnvFile.ValidateSecrets(values, out _));
    }

    [Theory]
    [InlineData(false, false)]
    [InlineData(false, true)]
    [InlineData(true, false)]
    [InlineData(true, true)]
    public async Task PreservesConfigurationAndBindsRepository(bool suppliedConfig, bool explicitRepository)
    {
        File.WriteAllText(Path.Combine(_root, "version.txt"), $"release_repository: {ForkRepository}\n");
        var inputPath = Path.Combine(_root, suppliedConfig ? "provided.env" : ".env");
        var values = EnvFile.BuildGenerated();
        values["SESSION_COOKIE_SECURE"] = "true";
        values["CUSTOM_SETTING"] = "preserved";
        values["PROTO_FLEET_RELEASE_REPOSITORY_BACKUP"] = "other/repo";
        values["export PROTO_FLEET_RELEASE_REPOSITORY_PREVIOUS"] = "another/repo";
        if (explicitRepository)
        {
            values[ReleaseSource.EnvironmentKey] = ForkRepository;
        }
        EnvFile.Write(inputPath, values);
        var original = File.ReadAllText(inputPath);

        var result = await new EnvConfigurator(new NoOpLogSink()).ConfigureAsync(
            Context(suppliedConfig ? inputPath : null), CancellationToken.None);

        Assert.True(result.Success, result.ErrorMessage);
        var persisted = EnvFile.Parse(Path.Combine(_root, ".env"));
        foreach (var pair in values)
        {
            Assert.Equal(pair.Value, persisted[pair.Key]);
        }
        Assert.Equal(ForkRepository, persisted[ReleaseSource.EnvironmentKey]);
        if (suppliedConfig)
        {
            Assert.Equal(original, File.ReadAllText(inputPath));
        }
    }

    [Theory]
    [InlineData(false, "export ")]
    [InlineData(true, "export ")]
    [InlineData(false, "  export\t  ")]
    [InlineData(true, "  export\t  ")]
    public async Task NormalizesExportedRepositoryWithoutDuplicatingAssignment(bool suppliedConfig, string prefix)
    {
        File.WriteAllText(Path.Combine(_root, "version.txt"), $"release_repository: {ForkRepository}\n");
        var inputPath = Path.Combine(_root, suppliedConfig ? "provided.env" : ".env");
        var values = EnvFile.BuildGenerated();
        values["CUSTOM_SETTING"] = "preserved";
        EnvFile.Write(inputPath, values);
        File.AppendAllText(inputPath, $"{prefix}{ReleaseSource.EnvironmentKey} = \"{ForkRepository}\" # source\r\n");
        var original = File.ReadAllText(inputPath);

        var result = await new EnvConfigurator(new NoOpLogSink()).ConfigureAsync(
            Context(suppliedConfig ? inputPath : null), CancellationToken.None);

        Assert.True(result.Success, result.ErrorMessage);
        var envPath = Path.Combine(_root, ".env");
        var persisted = EnvFile.Parse(envPath);
        foreach (var pair in values)
        {
            Assert.Equal(pair.Value, persisted[pair.Key]);
        }
        Assert.Equal(ForkRepository, persisted[ReleaseSource.EnvironmentKey]);
        Assert.Equal($"{ReleaseSource.EnvironmentKey}={ForkRepository}",
            Assert.Single(File.ReadAllLines(envPath).Where(line => line.Contains(ReleaseSource.EnvironmentKey, StringComparison.Ordinal))));
        ReleaseSource.CheckEnvironment(File.ReadAllText(envPath), ForkRepository);
        if (suppliedConfig)
        {
            Assert.Equal(original, File.ReadAllText(inputPath));
        }
    }

    [Theory]
    [InlineData(false, "export PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\r\n")]
    [InlineData(true, "export PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\r\n")]
    [InlineData(false, "export PROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\nPROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\n")]
    [InlineData(true, "export PROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\nPROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\n")]
    [InlineData(false, "PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\r\n")]
    [InlineData(true, "PROTO_FLEET_RELEASE_REPOSITORY=block/proto-fleet\r\n")]
    [InlineData(false, "PROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\nPROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\n")]
    [InlineData(true, "PROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\nPROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork\r\n")]
    public async Task RejectsConflictingOrDuplicateRepositoryWithoutOverwritingEnvironment(bool suppliedConfig, string assignment)
    {
        File.WriteAllText(Path.Combine(_root, "version.txt"), $"release_repository: {ForkRepository}\n");
        var envPath = Path.Combine(_root, ".env");
        var inputPath = Path.Combine(_root, suppliedConfig ? "provided.env" : ".env");
        EnvFile.Write(envPath, EnvFile.BuildGenerated());
        EnvFile.Write(inputPath, EnvFile.BuildGenerated());
        File.AppendAllText(inputPath, assignment);
        var original = File.ReadAllText(envPath);
        var configurator = new EnvConfigurator(new NoOpLogSink());

        await Assert.ThrowsAsync<InvalidDataException>(() => configurator.ConfigureAsync(
            Context(suppliedConfig ? inputPath : null), CancellationToken.None));

        Assert.Equal(original, File.ReadAllText(envPath));
    }

    [Fact]
    public async Task RejectsCrlfRepositoryMetadataWithoutOverwritingEnvironment()
    {
        File.WriteAllText(Path.Combine(_root, "version.txt"), $"release_repository: {ForkRepository}\r\n");
        var envPath = Path.Combine(_root, ".env");
        EnvFile.Write(envPath, EnvFile.BuildGenerated());
        var original = File.ReadAllText(envPath);

        await Assert.ThrowsAsync<InvalidDataException>(() =>
            new EnvConfigurator(new NoOpLogSink()).ConfigureAsync(Context(), CancellationToken.None));

        Assert.Equal(original, File.ReadAllText(envPath));
    }

    private InstallerContext Context(string? configPath = null) => new()
    {
        Options = new InstallerOptions { ConfigFilePath = configPath },
        DeploymentRootWindowsPath = _root
    };

    public void Dispose() => Directory.Delete(_root, recursive: true);

    private sealed class NoOpLogSink : ILogSink
    {
        public void Info(string message) { }
        public void Warn(string message) { }
        public void Error(string message) { }
    }
}
