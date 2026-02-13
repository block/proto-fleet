using FluentAssertions;
using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Core.Services;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class DeploymentResolverTests : IDisposable
{
    private readonly string _tempRoot;

    public DeploymentResolverTests()
    {
        _tempRoot = Path.Combine(Path.GetTempPath(), "protofleet-installer-tests", Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(_tempRoot);
    }

    [Fact]
    public async Task ResolveAsync_ShouldUseExplicitDeploymentPath()
    {
        var deploymentRoot = CreateDeploymentRoot(Path.Combine(_tempRoot, "release"));
        var resolver = new DeploymentResolver();
        var context = new InstallerContext
        {
            Options = new InstallerOptions
            {
                DeploymentPath = deploymentRoot
            }
        };

        var result = await resolver.ResolveAsync(context, CancellationToken.None);

        result.IsResolved.Should().BeTrue();
        result.DeploymentRootPath.Should().Be(deploymentRoot);
    }

    [Fact]
    public async Task ResolveAsync_ShouldAcceptValidTarballName()
    {
        var tarballPath = Path.Combine(_tempRoot, "proto-fleet-test.tar.gz");
        File.WriteAllText(tarballPath, "x");

        var resolver = new DeploymentResolver();
        var context = new InstallerContext
        {
            Options = new InstallerOptions
            {
                TarballPath = tarballPath
            }
        };

        var result = await resolver.ResolveAsync(context, CancellationToken.None);

        result.IsResolved.Should().BeTrue();
        result.TarballPath.Should().Be(tarballPath);
    }

    [Fact]
    public async Task ResolveAsync_ShouldRejectInvalidTarballName()
    {
        var tarballPath = Path.Combine(_tempRoot, "release.tar.gz");
        File.WriteAllText(tarballPath, "x");

        var resolver = new DeploymentResolver();
        var context = new InstallerContext
        {
            Options = new InstallerOptions
            {
                TarballPath = tarballPath
            }
        };

        var result = await resolver.ResolveAsync(context, CancellationToken.None);

        result.IsResolved.Should().BeFalse();
        result.FailureReason.Should().NotBeNullOrWhiteSpace();
    }

    [Fact]
    public async Task ResolveAsync_ShouldRejectConflictingExplicitSources()
    {
        var deploymentRoot = CreateDeploymentRoot(Path.Combine(_tempRoot, "release"));
        var tarballPath = Path.Combine(_tempRoot, "proto-fleet-test.tar.gz");
        File.WriteAllText(tarballPath, "x");

        var resolver = new DeploymentResolver();
        var context = new InstallerContext
        {
            Options = new InstallerOptions
            {
                DeploymentPath = deploymentRoot,
                TarballPath = tarballPath
            }
        };

        var result = await resolver.ResolveAsync(context, CancellationToken.None);

        result.IsResolved.Should().BeFalse();
        result.FailureReason.Should().Contain("Provide only one deployment source");
    }

    private static string CreateDeploymentRoot(string root)
    {
        Directory.CreateDirectory(root);
        Directory.CreateDirectory(Path.Combine(root, "server"));
        Directory.CreateDirectory(Path.Combine(root, "client"));
        File.WriteAllText(Path.Combine(root, "docker-compose.yaml"), "services:");
        return root;
    }

    public void Dispose()
    {
        try
        {
            Directory.Delete(_tempRoot, recursive: true);
        }
        catch
        {
            // no-op
        }
    }
}
