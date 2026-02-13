using Microsoft.Win32;
using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Windows;

public sealed class RunOnceResumeRegistrationService : IResumeRegistrationService
{
    private const string RunOncePath = @"Software\Microsoft\Windows\CurrentVersion\RunOnce";
    private const string ValueName = "ProtoFleetInstallerResume";

    public void RegisterAutoResume(string executablePath)
    {
        using var key = Registry.CurrentUser.CreateSubKey(RunOncePath);
        if (key is null)
        {
            throw new InvalidOperationException("Unable to open RunOnce registry key.");
        }

        key.SetValue(ValueName, $"\"{executablePath}\" -AutoStart true", RegistryValueKind.String);
    }

    public void ClearAutoResume()
    {
        try
        {
            using var key = Registry.CurrentUser.CreateSubKey(RunOncePath);
            key?.DeleteValue(ValueName, false);
        }
        catch
        {
            // no-op
        }
    }
}
