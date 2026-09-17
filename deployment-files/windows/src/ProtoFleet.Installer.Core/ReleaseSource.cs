using System.Formats.Tar;
using System.IO.Compression;
using System.Text.RegularExpressions;

namespace ProtoFleet.Installer.Core;

public static class ReleaseSource
{
    public const string DefaultRepository = "block/proto-fleet";
    public const string EnvironmentKey = "PROTO_FLEET_RELEASE_REPOSITORY";
    private static readonly Regex RepositoryPattern = new(
        @"\A[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?/[A-Za-z0-9][A-Za-z0-9._-]{0,99}\z");

    public static string FromMetadata(string metadata)
    {
        string? repository = null;
        // Raw bundle metadata follows the same strict format as Go and shell consumers.
        foreach (var line in metadata.Split('\n'))
        {
            if (!line.TrimStart().StartsWith("release_repository", StringComparison.Ordinal))
            {
                continue;
            }
            if (repository is not null || !line.StartsWith("release_repository: ", StringComparison.Ordinal))
            {
                throw new InvalidDataException("Invalid or duplicate release repository metadata.");
            }
            repository = line["release_repository: ".Length..];
            if (!RepositoryPattern.IsMatch(repository) || repository.Contains("..", StringComparison.Ordinal))
            {
                throw new InvalidDataException("Release repository must be a GitHub owner/repo value.");
            }
        }
        return repository ?? DefaultRepository;
    }

    public static string FromArchive(string path, bool allowMissingMetadata = false)
    {
        using var file = File.OpenRead(path);
        using var gzip = new GZipStream(file, CompressionMode.Decompress);
        using var tar = new TarReader(gzip);
        string? repository = null;
        while (tar.GetNextEntry() is { } entry)
        {
            if (entry.Name != "deployment/version.txt" && entry.Name != "./deployment/version.txt" && entry.Name != "version.txt" && entry.Name != "./version.txt")
            {
                continue;
            }
            if (repository is not null || entry.DataStream is null || entry.Length > 65536)
            {
                throw new InvalidDataException("Missing, duplicate or oversized release metadata.");
            }
            using var reader = new StreamReader(entry.DataStream, leaveOpen: true);
            repository = FromMetadata(reader.ReadToEnd());
        }
        if (repository is null && !allowMissingMetadata)
        {
            throw new InvalidDataException("Release bundle is missing version.txt.");
        }
        return repository ?? DefaultRepository;
    }

    public static void CheckConfiguredRepository(IReadOnlyDictionary<string, string> values, string repository)
    {
        if (values.TryGetValue(EnvironmentKey, out var configured) && configured != repository)
        {
            throw new InvalidDataException("Persisted release repository conflicts with the release bundle; cross-repository migration is not supported.");
        }
    }

    public static void CheckEnvironment(string contents, string repository)
    {
        string? configured = null;
        foreach (var line in contents.Split('\n'))
        {
            var normalized = line.Trim();
            if (normalized.StartsWith("export", StringComparison.Ordinal) && normalized.Length > 6 && char.IsWhiteSpace(normalized[6]))
            {
                normalized = normalized[6..].TrimStart();
            }
            if (!normalized.StartsWith(EnvironmentKey, StringComparison.Ordinal))
            {
                continue;
            }
            var assignment = normalized[EnvironmentKey.Length..];
            // A longer environment key is unrelated, even when it shares this prefix.
            if (assignment.Length > 0 && !char.IsWhiteSpace(assignment[0]) && assignment[0] != '=' && assignment[0] != ':')
            {
                continue;
            }
            assignment = assignment.TrimStart();
            if (configured is not null || assignment.Length == 0 || (assignment[0] != '=' && assignment[0] != ':'))
            {
                throw new InvalidDataException("Invalid or duplicate release repository configuration.");
            }
            var value = assignment[1..].Trim();
            var comment = value.IndexOf(" #", StringComparison.Ordinal);
            if (comment >= 0)
            {
                value = value[..comment].TrimEnd();
            }
            if (value.Length >= 2 && ((value[0] == '"' && value[^1] == '"') || (value[0] == '\'' && value[^1] == '\'')))
            {
                value = value[1..^1];
            }
            configured = value;
        }
        if (configured is not null)
        {
            CheckConfiguredRepository(new Dictionary<string, string> { [EnvironmentKey] = configured }, repository);
        }
    }
}
