using ProtoFleet.Installer.Core;
using System.Formats.Tar;
using System.IO.Compression;
using System.Text;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class ReleaseSourceTests
{
    [Theory]
    [InlineData("version: v1.0.0\n", "block/proto-fleet")]
    [InlineData("version: v1.0.0\nrelease_repository: example-owner/fleet-fork\n", "example-owner/fleet-fork")]
    [InlineData("release_repository: block/proto-fleet\ncommit: abc123\n", "block/proto-fleet")]
    public void ReadsOfficialLegacyAndForkMetadata(string metadata, string expected)
    {
        Assert.Equal(expected, ReleaseSource.FromMetadata(metadata));
    }

    [Theory]
    [InlineData("https://github.com/owner/repo")]
    [InlineData("owner/repo?token=secret")]
    [InlineData("owner/../repo")]
    [InlineData("owner/repo;id")]
    [InlineData(" owner/repo")]
    [InlineData("owner/repo ")]
    [InlineData("owner/repo\r\n")]
    [InlineData("owner/repo\t\r\n")]
    [InlineData("owner/repo\rmalformed")]
    [InlineData("owner/repo\nrelease_repository: other/repo")]
    public void RejectsInvalidMetadata(string repository)
    {
        Assert.Throws<InvalidDataException>(() => ReleaseSource.FromMetadata("release_repository: " + repository));
    }

    [Fact]
    public void RejectsConflictingConfiguration()
    {
        var values = new Dictionary<string, string> { [ReleaseSource.EnvironmentKey] = "block/proto-fleet" };
        Assert.Throws<InvalidDataException>(() => ReleaseSource.CheckConfiguredRepository(values, "example-owner/fleet-fork"));
    }

    [Theory]
    [InlineData("release_repository=example/fleet")]
    [InlineData(" release_repository: example/fleet")]
    public void RejectsMalformedMetadata(string metadata)
    {
        Assert.Throws<InvalidDataException>(() => ReleaseSource.FromMetadata(metadata));
    }

    [Theory]
    [InlineData("PROTO_FLEET_RELEASE_REPOSITORY=example-owner/fleet-fork")]
    [InlineData("PROTO_FLEET_RELEASE_REPOSITORY : example-owner/fleet-fork")]
    [InlineData("export PROTO_FLEET_RELEASE_REPOSITORY = \"example-owner/fleet-fork\" # comment")]
    public void ReadsEnvironmentIdentity(string environment)
    {
        ReleaseSource.CheckEnvironment(environment, "example-owner/fleet-fork");
        Assert.Throws<InvalidDataException>(() => ReleaseSource.CheckEnvironment(environment, "block/proto-fleet"));
        Assert.Throws<InvalidDataException>(() => ReleaseSource.CheckEnvironment(environment + "\n" + environment, "example-owner/fleet-fork"));
    }

    [Theory]
    [InlineData("PROTO_FLEET_RELEASE_REPOSITORY_BACKUP=other/repo")]
    [InlineData("export PROTO_FLEET_RELEASE_REPOSITORY_BACKUP = other/repo")]
    [InlineData("PROTO_FLEET_RELEASE_REPOSITORY2: other/repo")]
    public void IgnoresEnvironmentKeysSharingRepositoryPrefix(string unrelated)
    {
        const string repository = "example-owner/fleet-fork";
        var matching = $"{ReleaseSource.EnvironmentKey}={repository}";
        ReleaseSource.CheckEnvironment(unrelated, repository);
        ReleaseSource.CheckEnvironment(unrelated + "\n" + matching, repository);
        ReleaseSource.CheckEnvironment(matching + "\n" + unrelated, repository);
        Assert.Throws<InvalidDataException>(() => ReleaseSource.CheckEnvironment(
            unrelated + "\n" + matching, ReleaseSource.DefaultRepository));
        Assert.Throws<InvalidDataException>(() => ReleaseSource.CheckEnvironment(
            matching + "\n" + unrelated + "\n" + matching, repository));
    }

    [Theory]
    [InlineData("PROTO_FLEET_RELEASE_REPOSITORY")]
    [InlineData("PROTO_FLEET_RELEASE_REPOSITORY missing-delimiter")]
    [InlineData("export PROTO_FLEET_RELEASE_REPOSITORY =")]
    public void RejectsMalformedExactRepositorySetting(string environment)
    {
        Assert.Throws<InvalidDataException>(() => ReleaseSource.CheckEnvironment(environment, "example-owner/fleet-fork"));
    }

    [Theory]
    [InlineData(0, "\n")]
    [InlineData(1, "\n")]
    [InlineData(2, "\n")]
    [InlineData(1, "\r\n")]
    public void ReadsArchiveIdentityAndOnlyAllowsMissingMetadataWhenRequested(int count, string lineEnding)
    {
        var path = Path.Combine(Path.GetTempPath(), $"release-source-{Guid.NewGuid():N}.tar.gz");
        try
        {
            using (var file = File.Create(path))
            using (var gzip = new GZipStream(file, CompressionMode.Compress))
            using (var writer = new TarWriter(gzip))
            {
                // Keep the bundle valid even when version.txt is intentionally absent.
                using var compose = new MemoryStream(Encoding.UTF8.GetBytes("services: {}\n"));
                writer.WriteEntry(new PaxTarEntry(TarEntryType.RegularFile, "deployment/docker-compose.yaml") { DataStream = compose });

                for (var index = 0; index < count; index++)
                {
                    using var metadata = new MemoryStream(Encoding.UTF8.GetBytes($"version: v1.0.0{lineEnding}release_repository: example-owner/fleet-fork{lineEnding}"));
                    var entry = new PaxTarEntry(TarEntryType.RegularFile, "deployment/version.txt") { DataStream = metadata };
                    writer.WriteEntry(entry);
                }
            }
            if (count == 1 && lineEnding == "\n")
            {
                Assert.Equal("example-owner/fleet-fork", ReleaseSource.FromArchive(path));
                Assert.Equal("example-owner/fleet-fork", ReleaseSource.FromArchive(path, allowMissingMetadata: true));
            }
            else
            {
                Assert.Throws<InvalidDataException>(() => ReleaseSource.FromArchive(path));
                if (count == 0)
                {
                    Assert.Equal(ReleaseSource.DefaultRepository, ReleaseSource.FromArchive(path, allowMissingMetadata: true));
                }
                else
                {
                    Assert.Throws<InvalidDataException>(() => ReleaseSource.FromArchive(path, allowMissingMetadata: true));
                }
            }
        }
        finally
        {
            File.Delete(path);
        }
    }
}
