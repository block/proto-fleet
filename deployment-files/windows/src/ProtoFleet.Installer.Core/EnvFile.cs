using System.Security.Cryptography;
using System.Text;

namespace ProtoFleet.Installer.Core;

public static class EnvFile
{
    private static readonly string[] RequiredKeys =
    [
        "MYSQL_ROOT_PASSWORD",
        "DB_USERNAME",
        "DB_PASSWORD",
        "AUTH_CLIENT_SECRET_KEY",
        "ENCRYPT_SERVICE_MASTER_KEY"
    ];

    public static Dictionary<string, string> Parse(string path)
    {
        var map = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        foreach (var line in File.ReadAllLines(path))
        {
            if (string.IsNullOrWhiteSpace(line) || line.TrimStart().StartsWith('#'))
            {
                continue;
            }

            var index = line.IndexOf('=');
            if (index < 1)
            {
                continue;
            }

            var key = line[..index].Trim();
            var value = line[(index + 1)..].Trim();
            map[key] = value;
        }

        return map;
    }

    public static bool HasRequiredKeys(IReadOnlyDictionary<string, string> values, out IReadOnlyList<string> missingKeys)
    {
        var missing = RequiredKeys.Where(key => !values.ContainsKey(key) || string.IsNullOrWhiteSpace(values[key])).ToList();
        missingKeys = missing;
        return missing.Count == 0;
    }

    public static bool ValidateSecrets(IReadOnlyDictionary<string, string> values, out string? error)
    {
        error = null;
        if (values.TryGetValue("AUTH_CLIENT_SECRET_KEY", out var authSecret) && authSecret.Length < 32)
        {
            error = "AUTH_CLIENT_SECRET_KEY must be at least 32 characters.";
            return false;
        }

        if (!values.TryGetValue("ENCRYPT_SERVICE_MASTER_KEY", out var encryptKey))
        {
            error = "ENCRYPT_SERVICE_MASTER_KEY is missing.";
            return false;
        }

        try
        {
            var bytes = Convert.FromBase64String(encryptKey);
            if (bytes.Length != 32)
            {
                error = "ENCRYPT_SERVICE_MASTER_KEY must decode to 32 bytes.";
                return false;
            }
        }
        catch (FormatException)
        {
            error = "ENCRYPT_SERVICE_MASTER_KEY must be valid base64.";
            return false;
        }

        return true;
    }

    public static Dictionary<string, string> BuildGenerated()
    {
        var values = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
        {
            ["MYSQL_ROOT_PASSWORD"] = GenerateAlphaNumeric(32),
            ["DB_USERNAME"] = "protofleet",
            ["DB_PASSWORD"] = GenerateAlphaNumeric(32),
            ["AUTH_CLIENT_SECRET_KEY"] = GenerateAlphaNumeric(48),
            ["ENCRYPT_SERVICE_MASTER_KEY"] = Convert.ToBase64String(RandomNumberGenerator.GetBytes(32)),
        };

        return values;
    }

    public static void Write(string path, IReadOnlyDictionary<string, string> values)
    {
        var content = new StringBuilder();
        foreach (var pair in values.OrderBy(x => x.Key, StringComparer.Ordinal))
        {
            content.AppendLine($"{pair.Key}={pair.Value}");
        }

        File.WriteAllText(path, content.ToString(), Encoding.UTF8);
    }

    public static string GenerateAlphaNumeric(int length)
    {
        const string chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
        var bytes = RandomNumberGenerator.GetBytes(length);
        var result = new StringBuilder(length);
        for (var i = 0; i < length; i++)
        {
            result.Append(chars[bytes[i] % chars.Length]);
        }

        return result.ToString();
    }
}
