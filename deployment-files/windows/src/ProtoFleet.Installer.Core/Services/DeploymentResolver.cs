using System.Text.RegularExpressions;

namespace ProtoFleet.Installer.Core.Services;

public sealed class DeploymentResolver : IDeploymentResolver
{
    private static readonly Regex TarballNameRegex = new(@"^proto-fleet-.*\.tar\.gz$", RegexOptions.IgnoreCase | RegexOptions.Compiled);

    public Task<DeploymentResolution> ResolveAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        cancellationToken.ThrowIfCancellationRequested();

        var explicitPath = context.Options.DeploymentPath;
        var explicitTarball = context.Options.TarballPath;
        if (!string.IsNullOrWhiteSpace(explicitPath) && !string.IsNullOrWhiteSpace(explicitTarball))
        {
            return Task.FromResult(new DeploymentResolution
            {
                IsResolved = false,
                FailureReason =
                    "Both -DeploymentPath and -TarballPath were provided. " +
                    "Provide only one deployment source."
            });
        }

        if (!string.IsNullOrWhiteSpace(explicitTarball))
        {
            return Task.FromResult(ValidateTarball(explicitTarball));
        }

        if (!string.IsNullOrWhiteSpace(explicitPath))
        {
            var candidate = ResolveToDeploymentRoot(explicitPath);
            if (candidate is not null)
            {
                return Task.FromResult(new DeploymentResolution
                {
                    IsResolved = true,
                    DeploymentRootPath = candidate
                });
            }

            return Task.FromResult(new DeploymentResolution
            {
                IsResolved = false,
                FailureReason = $"-DeploymentPath '{explicitPath}' is not a valid deployment root."
            });
        }

        var envFallback = Environment.GetEnvironmentVariable("PROTOFLEET_DEPLOYMENT_PATH");
        if (!string.IsNullOrWhiteSpace(envFallback))
        {
            var candidate = ResolveToDeploymentRoot(envFallback);
            if (candidate is not null)
            {
                return Task.FromResult(new DeploymentResolution
                {
                    IsResolved = true,
                    DeploymentRootPath = candidate
                });
            }
        }

        var exePath = AppContext.BaseDirectory;
        var cwdPath = Environment.CurrentDirectory;
        foreach (var seed in new[] { exePath, cwdPath })
        {
            var candidate = ResolveToDeploymentRoot(seed);
            if (candidate is not null)
            {
                return Task.FromResult(new DeploymentResolution
                {
                    IsResolved = true,
                    DeploymentRootPath = candidate
                });
            }
        }

        return Task.FromResult(new DeploymentResolution
        {
            IsResolved = false,
            FailureReason = "No deployment root found and tarball input is missing or invalid."
        });
    }

    private static string? ResolveToDeploymentRoot(string rawPath)
    {
        var fullPath = Normalize(rawPath);
        if (fullPath is null)
        {
            return null;
        }

        if (File.Exists(fullPath))
        {
            fullPath = Path.GetDirectoryName(fullPath);
            if (string.IsNullOrWhiteSpace(fullPath))
            {
                return null;
            }
        }

        var current = new DirectoryInfo(fullPath);
        while (current is not null)
        {
            if (LooksLikeDeploymentRoot(current.FullName))
            {
                return current.FullName;
            }

            var deploymentChild = Path.Combine(current.FullName, "deployment");
            if (LooksLikeDeploymentRoot(deploymentChild))
            {
                return deploymentChild;
            }

            current = current.Parent;
        }

        return null;
    }

    private static DeploymentResolution ValidateTarball(string tarballPath)
    {
        var normalized = Normalize(tarballPath);
        if (normalized is null || !File.Exists(normalized))
        {
            return new DeploymentResolution
            {
                IsResolved = false,
                FailureReason = $"Tarball path '{tarballPath}' does not exist."
            };
        }

        var fileName = Path.GetFileName(normalized);
        if (!TarballNameRegex.IsMatch(fileName))
        {
            return new DeploymentResolution
            {
                IsResolved = false,
                FailureReason = $"Tarball '{fileName}' must match proto-fleet-*.tar.gz."
            };
        }

        return new DeploymentResolution
        {
            IsResolved = true,
            TarballPath = normalized
        };
    }

    private static bool LooksLikeDeploymentRoot(string path)
    {
        return Directory.Exists(path) &&
               File.Exists(Path.Combine(path, "docker-compose.yaml")) &&
               Directory.Exists(Path.Combine(path, "server")) &&
               Directory.Exists(Path.Combine(path, "client"));
    }

    private static string? Normalize(string? rawPath)
    {
        if (string.IsNullOrWhiteSpace(rawPath))
        {
            return null;
        }

        try
        {
            return Path.GetFullPath(rawPath);
        }
        catch
        {
            return null;
        }
    }
}
