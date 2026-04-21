namespace ProtoFleet.Installer.Core;

public static class ScheduledTaskCommandBuilder
{
    public static string BuildTaskAction(string distroName)
    {
        var quotedDistro = CommandEscaping.WindowsArgument(distroName);
        return $"wsl.exe -d {quotedDistro} -u root -- bash -lc 'systemctl start docker || service docker start'";
    }
}
