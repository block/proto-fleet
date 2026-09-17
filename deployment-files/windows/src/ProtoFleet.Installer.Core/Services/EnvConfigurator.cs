namespace ProtoFleet.Installer.Core.Services;

public sealed class EnvConfigurator : IEnvConfigurator
{
    private readonly ILogSink _logSink;

    public EnvConfigurator(ILogSink logSink)
    {
        _logSink = logSink;
    }

    public Task<InstallerStepResult> ConfigureAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        cancellationToken.ThrowIfCancellationRequested();

        if (string.IsNullOrWhiteSpace(context.DeploymentRootWindowsPath))
        {
            return Task.FromResult(InstallerStepResult.Failed("Deployment root was not resolved on Windows side."));
        }

        var envPath = Path.Combine(context.DeploymentRootWindowsPath, ".env");
        var metadataPath = Path.Combine(context.DeploymentRootWindowsPath, "version.txt");
        var repository = ReleaseSource.FromMetadata(File.Exists(metadataPath) ? File.ReadAllText(metadataPath) : "");
        Dictionary<string, string> values;

        if (!string.IsNullOrWhiteSpace(context.Options.ConfigFilePath))
        {
            var configPath = Path.GetFullPath(context.Options.ConfigFilePath);
            if (!File.Exists(configPath))
            {
                return Task.FromResult(InstallerStepResult.Failed($"Config file '{configPath}' does not exist."));
            }

            values = EnvFile.Parse(configPath);
            ReleaseSource.CheckEnvironment(File.ReadAllText(configPath), repository);
            if (!EnvFile.HasRequiredKeys(values, out var missing))
            {
                return Task.FromResult(InstallerStepResult.Failed($"Provided .env is missing required keys: {string.Join(", ", missing)}"));
            }

            if (!EnvFile.ValidateSecrets(values, out var secretError))
            {
                return Task.FromResult(InstallerStepResult.Failed(secretError ?? "Provided .env secret validation failed."));
            }
        }
        else if (File.Exists(envPath))
        {
            values = EnvFile.Parse(envPath);
            ReleaseSource.CheckEnvironment(File.ReadAllText(envPath), repository);
            if (!EnvFile.HasRequiredKeys(values, out _))
            {
                values = MergeGenerated(values);
                _logSink.Warn("Existing .env was incomplete. Missing required keys were generated.");
            }
        }
        else
        {
            values = EnvFile.BuildGenerated();
        }

        if (!values.ContainsKey("SESSION_COOKIE_SECURE"))
        {
            values["SESSION_COOKIE_SECURE"] = "false";
        }

        // CheckEnvironment accepts exported assignments, but EnvFile preserves
        // their prefix in the key. Remove that alias before writing one canonical key.
        foreach (var key in values.Keys.Where(key =>
            key.StartsWith("export", StringComparison.Ordinal) && key.Length > 6 && char.IsWhiteSpace(key[6]) &&
            key[6..].TrimStart() == ReleaseSource.EnvironmentKey).ToArray())
        {
            values.Remove(key);
        }
        values[ReleaseSource.EnvironmentKey] = repository;
        EnvFile.Write(envPath, values);

        return Task.FromResult(InstallerStepResult.Succeeded());
    }

    private static Dictionary<string, string> MergeGenerated(Dictionary<string, string> existing)
    {
        var generated = EnvFile.BuildGenerated();
        foreach (var pair in generated)
        {
            if (!existing.ContainsKey(pair.Key) || string.IsNullOrWhiteSpace(existing[pair.Key]))
            {
                existing[pair.Key] = pair.Value;
            }
        }

        return existing;
    }
}
