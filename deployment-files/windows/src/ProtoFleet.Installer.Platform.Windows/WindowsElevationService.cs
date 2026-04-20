using System.Diagnostics;
using System.Security.Principal;
using ProtoFleet.Installer.Core;

namespace ProtoFleet.Installer.Platform.Windows;

public sealed class WindowsElevationService
{
    public bool IsAdministrator()
    {
        using var identity = WindowsIdentity.GetCurrent();
        var principal = new WindowsPrincipal(identity);
        return principal.IsInRole(WindowsBuiltInRole.Administrator);
    }

    public void RelaunchElevatedAndExit(string[] args)
    {
        var currentExe = Environment.ProcessPath
            ?? throw new InvalidOperationException("Cannot locate current executable path.");

        var quotedArgs = string.Join(" ", args.Select(CommandEscaping.WindowsArgument));
        var startInfo = new ProcessStartInfo
        {
            FileName = currentExe,
            Arguments = quotedArgs,
            Verb = "runas",
            UseShellExecute = true,
        };

        try
        {
            Process.Start(startInfo);
            Environment.Exit(0);
        }
        catch
        {
            // UAC prompt cancellation or shell launch failure.
        }
    }
}
