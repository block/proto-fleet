namespace ProtoFleet.Installer.Core;

public static class CommandEscaping
{
    public static string WindowsArgument(string arg)
    {
        if (string.IsNullOrWhiteSpace(arg))
        {
            return "\"\"";
        }

        if (!arg.Any(char.IsWhiteSpace) && !arg.Contains('"'))
        {
            return arg;
        }

        return $"\"{arg.Replace("\"", "\\\"")}\"";
    }

    public static string BashSingleQuote(string value)
    {
        return $"'{value.Replace("'", "'\"'\"'")}'";
    }

    public static string PowerShellSingleQuote(string value)
    {
        return $"'{value.Replace("'", "''")}'";
    }
}
