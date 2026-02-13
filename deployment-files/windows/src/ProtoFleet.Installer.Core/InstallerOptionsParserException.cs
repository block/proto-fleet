namespace ProtoFleet.Installer.Core;

public sealed class InstallerOptionsParserException : Exception
{
    public InstallerOptionsParserException(string message)
        : base(message)
    {
    }
}
