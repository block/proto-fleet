namespace ProtoFleet.Installer.Platform.Wsl;

public static class WslOutputClassifier
{
    public static bool LooksDnsIssue(string output)
    {
        return output.Contains("Temporary failure resolving", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Could not resolve host", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("server misbehaving", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("lookup registry-1.docker.io on 127.0.0.53", StringComparison.OrdinalIgnoreCase);
    }

    public static bool LooksAptRepositoryReachabilityIssue(string output)
    {
        return output.Contains("Failed to fetch", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Could not connect to", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Connection timed out", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Network is unreachable", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Name or service not known", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("Temporary failure resolving", StringComparison.OrdinalIgnoreCase);
    }

    public static bool LooksTlsOrCacheIssue(string output)
    {
        return output.Contains("tls: bad record MAC", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("failed to compute cache key", StringComparison.OrdinalIgnoreCase);
    }

    public static bool LooksDockerCliMissing(string output)
    {
        return output.Contains("docker: command not found", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("docker compose: not found", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("executable file not found", StringComparison.OrdinalIgnoreCase);
    }

    public static bool LooksDockerDaemonUnavailable(string output)
    {
        return output.Contains("cannot connect to the docker daemon", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("is the docker daemon running", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("error during connect", StringComparison.OrdinalIgnoreCase);
    }

    public static bool LooksFalseNegativeComposeBuild(string output)
    {
        return output.Contains("naming to", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("writing image", StringComparison.OrdinalIgnoreCase) ||
               output.Contains("exporting layers", StringComparison.OrdinalIgnoreCase);
    }
}
