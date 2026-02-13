namespace ProtoFleet.Installer.Core;

public static class InstallerOptionsParser
{
    private static readonly string[] DeprecatedLinuxUserArgs =
    [
        "-LinuxUserMode",
        "-LinuxUsername",
        "-LinuxPassword",
    ];

    public static InstallerOptions Parse(IReadOnlyList<string> args)
    {
        var values = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        for (var i = 0; i < args.Count; i++)
        {
            var arg = args[i];
            if (!arg.StartsWith('-'))
            {
                continue;
            }

            if (i + 1 >= args.Count || args[i + 1].StartsWith('-'))
            {
                values[arg] = "true";
                continue;
            }

            values[arg] = args[i + 1];
            i++;
        }

        var deprecatedArgsUsed = DeprecatedLinuxUserArgs
            .Where(values.ContainsKey)
            .OrderBy(x => x, StringComparer.OrdinalIgnoreCase)
            .ToArray();
        if (deprecatedArgsUsed.Length > 0)
        {
            throw new InstallerOptionsParserException(
                "Linux user CLI arguments are no longer supported. " +
                "Ubuntu user provisioning is interactive-only. " +
                $"Remove: {string.Join(", ", deprecatedArgsUsed)}.");
        }

        var mode = ParseMode(values.TryGetValue("-Guided", out var guided) ? guided : null);
        var protocolMode = ParseProtocolMode(values.TryGetValue("-ProtocolMode", out var protocol) ? protocol : null);

        return new InstallerOptions
        {
            VersionLabel = values.TryGetValue("-Version", out var version) ? version : "latest",
            DeploymentPath = values.TryGetValue("-DeploymentPath", out var deploymentPath) ? deploymentPath : null,
            TarballPath = values.TryGetValue("-TarballPath", out var tarballPath) ? tarballPath : null,
            ConfigFilePath = values.TryGetValue("-ConfigFile", out var configFilePath) ? configFilePath : null,
            InstallDir = values.TryGetValue("-InstallDir", out var installDir) ? installDir : "~/proto-fleet",
            SetupMode = mode,
            ProtocolMode = protocolMode,
            ExistingCertPath = values.TryGetValue("-CertPath", out var certPath) ? certPath : null,
            ExistingKeyPath = values.TryGetValue("-KeyPath", out var keyPath) ? keyPath : null,
            EnableAutoStartTask = values.TryGetValue("-EnableAutoStartTask", out var autoStartRaw) && ParseBool(autoStartRaw),
            AutoStart = values.TryGetValue("-AutoStart", out var resumeRaw) && ParseBool(resumeRaw),
            Debug = values.TryGetValue("-Debug", out var debugRaw) && ParseBool(debugRaw),
        };
    }

    private static SetupMode ParseMode(string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return SetupMode.Simple;
        }

        return ParseBool(value) ? SetupMode.Guided : SetupMode.Simple;
    }

    private static ProtocolMode ParseProtocolMode(string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return ProtocolMode.Auto;
        }

        return value.Trim().ToLowerInvariant() switch
        {
            "http" => ProtocolMode.Http,
            "https-self-signed" => ProtocolMode.HttpsSelfSigned,
            "https-existing" => ProtocolMode.HttpsExisting,
            _ => ProtocolMode.Auto,
        };
    }

    private static bool ParseBool(string? raw)
    {
        if (string.IsNullOrWhiteSpace(raw))
        {
            return false;
        }

        return raw.Equals("true", StringComparison.OrdinalIgnoreCase) ||
               raw.Equals("1", StringComparison.OrdinalIgnoreCase) ||
               raw.Equals("yes", StringComparison.OrdinalIgnoreCase);
    }
}
