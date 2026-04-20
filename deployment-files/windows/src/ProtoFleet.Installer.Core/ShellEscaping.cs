namespace ProtoFleet.Installer.Core;

public static class ShellEscaping
{
    public static string BashSingleQuote(string value)
    {
        return CommandEscaping.BashSingleQuote(value);
    }
}
