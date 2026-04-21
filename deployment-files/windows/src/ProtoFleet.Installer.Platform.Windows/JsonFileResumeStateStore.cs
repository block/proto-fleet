using System.Text.Json;
using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Windows;

public sealed class JsonFileResumeStateStore : IInstallerResumeStateStore
{
    private readonly string _path;
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNameCaseInsensitive = true,
        WriteIndented = true
    };

    public JsonFileResumeStateStore(string? path = null)
    {
        _path = path ?? Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData),
            "ProtoFleet",
            "resume-state.json");
    }

    public InstallerResumeState? Load()
    {
        try
        {
            if (!File.Exists(_path))
            {
                return null;
            }

            var json = File.ReadAllText(_path);
            var state = JsonSerializer.Deserialize<InstallerResumeState>(json, JsonOptions);
            return state?.Options is null ? null : state;
        }
        catch
        {
            return null;
        }
    }

    public void Save(InstallerResumeState state)
    {
        var directory = Path.GetDirectoryName(_path);
        if (!string.IsNullOrWhiteSpace(directory))
        {
            Directory.CreateDirectory(directory);
        }

        var json = JsonSerializer.Serialize(state, JsonOptions);
        File.WriteAllText(_path, json);
    }

    public void Clear()
    {
        try
        {
            if (File.Exists(_path))
            {
                File.Delete(_path);
            }
        }
        catch
        {
            // no-op
        }
    }
}
