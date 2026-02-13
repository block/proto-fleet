using ProtoFleet.Installer.Core;
using System.Diagnostics;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class DeploymentPreparationService : IDeploymentPreparationService
{
    private readonly WslCommandExecutor _executor;
    private readonly ILogSink _logSink;

    public DeploymentPreparationService(WslCommandExecutor executor, ILogSink logSink)
    {
        _executor = executor;
        _logSink = logSink;
    }

    public async Task<InstallerStepResult> PrepareAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        if (string.IsNullOrWhiteSpace(context.SelectedDistro))
        {
            return InstallerStepResult.Failed("WSL distro was not selected before deployment preparation.");
        }

        if (!string.IsNullOrWhiteSpace(context.TarballPath))
        {
            return await PrepareFromTarballAsync(context, cancellationToken);
        }

        if (string.IsNullOrWhiteSpace(context.DeploymentRootWindowsPath))
        {
            return InstallerStepResult.Failed("Deployment root path is not set.");
        }

        return await PrepareFromWindowsPathAsync(context, cancellationToken);
    }

    private async Task<InstallerStepResult> PrepareFromTarballAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        return await PrepareFromTarballPathAsync(context, context.TarballPath!, cancellationToken);
    }

    private async Task<InstallerStepResult> PrepareFromWindowsPathAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        var sourceWindowsPath = Path.GetFullPath(context.DeploymentRootWindowsPath!);
        if (!Directory.Exists(sourceWindowsPath))
        {
            return InstallerStepResult.Failed($"Deployment directory does not exist: {sourceWindowsPath}");
        }

        string? tempTarball = null;
        try
        {
            tempTarball = CreateTempTarballPath();
            _logSink.Info($"Creating temporary deployment tarball: {tempTarball}");
            CreateTarballFromDirectory(sourceWindowsPath, tempTarball);
            return await PrepareFromTarballPathAsync(context, tempTarball, cancellationToken);
        }
        catch (Exception ex)
        {
            return InstallerStepResult.Failed($"Failed to package deployment for WSL transfer. {ex.Message}");
        }
        finally
        {
            TryDeleteFile(tempTarball);
        }
    }

    private async Task<InstallerStepResult> PrepareFromTarballPathAsync(
        InstallerContext context,
        string tarballPath,
        CancellationToken cancellationToken)
    {
        var distro = context.SelectedDistro!;
        var tarballWindowsPath = Path.GetFullPath(tarballPath);

        var wslPath = await ConvertToWslPathAsync(distro, tarballWindowsPath, cancellationToken);
        if (string.IsNullOrWhiteSpace(wslPath))
        {
            return InstallerStepResult.Failed("Could not resolve tarball path inside WSL.");
        }

        var installDirExpr = BuildInstallDirExpression(context.Options.InstallDir);
        var extractCmd = "set -e; " +
                         $"INSTALL_DIR={installDirExpr}; " +
                         "BACKUP_ENV=/tmp/protofleet-influx.env.backup; " +
                         "if [ -f \"$INSTALL_DIR/deployment/server/influx_config/.env\" ]; then cp \"$INSTALL_DIR/deployment/server/influx_config/.env\" \"$BACKUP_ENV\"; fi; " +
                         "mkdir -p \"$INSTALL_DIR\"; " +
                         $"tar -xzf {ShellEscaping.BashSingleQuote(wslPath)} -C \"$INSTALL_DIR\"; " +
                         "if [ -f \"$BACKUP_ENV\" ] && [ -d \"$INSTALL_DIR/deployment/server/influx_config\" ]; then cp \"$BACKUP_ENV\" \"$INSTALL_DIR/deployment/server/influx_config/.env\"; fi";
        var extract = await _executor.RunInDistroAsync(distro, extractCmd, asRoot: false, cancellationToken);
        if (!extract.IsSuccess)
        {
            return InstallerStepResult.Failed("Failed to extract tarball in WSL.");
        }

        var detectCmd =
            $"INSTALL_DIR={installDirExpr}; " +
            "if [ -f \"$INSTALL_DIR/deployment/docker-compose.yaml\" ]; then " +
            "echo \"$INSTALL_DIR/deployment\"; " +
            "elif [ -f \"$INSTALL_DIR/docker-compose.yaml\" ]; then " +
            "echo \"$INSTALL_DIR\"; else exit 1; fi";
        var detect = await _executor.RunInDistroAsync(distro, detectCmd, asRoot: false, cancellationToken);
        if (!detect.IsSuccess)
        {
            return InstallerStepResult.Failed("Tarball extraction completed but deployment root was not found.");
        }

        var deploymentWsl = detect.StandardOutput.Trim();
        var deploymentWindows = ToWslUncPath(distro, deploymentWsl);
        context.DeploymentRootWslPath = deploymentWsl;
        context.DeploymentRootWindowsPath = deploymentWindows;
        context.DeploymentTransferMode = "tar-copy";
        context.Checkpoint = InstallerCheckpoint.DeploymentPreparation;
        Directory.CreateDirectory(deploymentWindows);
        return InstallerStepResult.Succeeded();
    }

    private async Task<string?> ConvertToWslPathAsync(string distro, string windowsPath, CancellationToken cancellationToken)
    {
        var result = await _executor.RunInDistroAsync(
            distro,
            $"wslpath -a {ShellEscaping.BashSingleQuote(windowsPath)}",
            asRoot: false,
            cancellationToken);
        if (!result.IsSuccess)
        {
            return TryManualPathConvert(windowsPath);
        }

        return result.StandardOutput.Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries).FirstOrDefault()
               ?? TryManualPathConvert(windowsPath);
    }

    private static string? TryManualPathConvert(string windowsPath)
    {
        if (string.IsNullOrWhiteSpace(windowsPath) || windowsPath.Length < 2 || windowsPath[1] != ':')
        {
            return null;
        }

        var drive = char.ToLowerInvariant(windowsPath[0]);
        var remainder = windowsPath[2..].Replace('\\', '/');
        return $"/mnt/{drive}{remainder}";
    }

    private static string ToWslUncPath(string distro, string linuxPath)
    {
        var parts = linuxPath.Trim().Trim('/').Split('/', StringSplitOptions.RemoveEmptyEntries);
        return $@"\\wsl$\{distro}\{string.Join('\\', parts)}";
    }

    private static string CreateTempTarballPath()
    {
        return Path.Combine(
            Path.GetTempPath(),
            $"proto-fleet-deployment-{Guid.NewGuid():N}.tar.gz");
    }

    private static void CreateTarballFromDirectory(string sourceDirectory, string tarballPath)
    {
        var parent = Path.GetDirectoryName(sourceDirectory);
        var name = Path.GetFileName(sourceDirectory);
        if (string.IsNullOrWhiteSpace(parent) || string.IsNullOrWhiteSpace(name))
        {
            throw new InvalidOperationException($"Could not create tarball from path: {sourceDirectory}");
        }

        var process = new Process
        {
            StartInfo = new ProcessStartInfo
            {
                FileName = "tar.exe",
                Arguments =
                    $"-czf {CommandEscaping.WindowsArgument(tarballPath)} " +
                    $"-C {CommandEscaping.WindowsArgument(parent)} " +
                    $"{CommandEscaping.WindowsArgument(name)}",
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                UseShellExecute = false,
                CreateNoWindow = true,
            }
        };

        if (!process.Start())
        {
            throw new InvalidOperationException("Failed to launch tar.exe while creating deployment archive.");
        }

        if (!process.WaitForExit(300_000))
        {
            try { process.Kill(entireProcessTree: true); } catch { }
            throw new TimeoutException("Timed out creating deployment archive tarball.");
        }

        if (process.ExitCode != 0)
        {
            var stderr = process.StandardError.ReadToEnd().Trim();
            var stdout = process.StandardOutput.ReadToEnd().Trim();
            throw new InvalidOperationException(
                $"tar.exe failed (exit {process.ExitCode}). stderr={stderr} stdout={stdout}");
        }
    }

    private static void TryDeleteFile(string? path)
    {
        if (string.IsNullOrWhiteSpace(path) || !File.Exists(path))
        {
            return;
        }

        try
        {
            File.Delete(path);
        }
        catch
        {
            // no-op
        }
    }

    private static string BuildInstallDirExpression(string installDir)
    {
        if (string.IsNullOrWhiteSpace(installDir))
        {
            return "\"$HOME/proto-fleet\"";
        }

        if (installDir.StartsWith("~/", StringComparison.Ordinal))
        {
            var relative = installDir[2..].Replace("\"", string.Empty);
            return $"\"$HOME/{relative}\"";
        }

        if (installDir == "~")
        {
            return "\"$HOME\"";
        }

        return ShellEscaping.BashSingleQuote(installDir);
    }
}
