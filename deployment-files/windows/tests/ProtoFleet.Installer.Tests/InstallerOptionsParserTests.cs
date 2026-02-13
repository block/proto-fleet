using FluentAssertions;
using ProtoFleet.Installer.Core;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class InstallerOptionsParserTests
{
    [Fact]
    public void Parse_ParsesKnownArguments()
    {
        var args = new[]
        {
            "-DeploymentPath", "C:\\tmp\\deployment",
            "-TarballPath", "C:\\tmp\\proto-fleet-1.0.0.tar.gz",
            "-ConfigFile", "C:\\tmp\\.env",
            "-InstallDir", "~/proto-fleet",
            "-Guided", "true",
            "-ProtocolMode", "https-self-signed",
            "-EnableAutoStartTask", "1",
            "-AutoStart", "true"
        };

        var options = InstallerOptionsParser.Parse(args);

        options.DeploymentPath.Should().Be("C:\\tmp\\deployment");
        options.TarballPath.Should().Be("C:\\tmp\\proto-fleet-1.0.0.tar.gz");
        options.ConfigFilePath.Should().Be("C:\\tmp\\.env");
        options.SetupMode.Should().Be(SetupMode.Guided);
        options.ProtocolMode.Should().Be(ProtocolMode.HttpsSelfSigned);
        options.EnableAutoStartTask.Should().BeTrue();
        options.AutoStart.Should().BeTrue();
    }

    [Fact]
    public void Parse_DefaultsToSimpleMode()
    {
        var options = InstallerOptionsParser.Parse(Array.Empty<string>());

        options.SetupMode.Should().Be(SetupMode.Simple);
        options.ProtocolMode.Should().Be(ProtocolMode.Auto);
    }

    [Fact]
    public void Parse_GuidedParsesSuccessfully()
    {
        var options = InstallerOptionsParser.Parse(new[] { "-Guided", "true" });

        options.SetupMode.Should().Be(SetupMode.Guided);
    }

    [Theory]
    [InlineData("-LinuxUserMode", "prompt")]
    [InlineData("-LinuxUsername", "fleetadmin")]
    [InlineData("-LinuxPassword", "Secret123")]
    public void Parse_ThrowsForDeprecatedLinuxUserArguments(string key, string value)
    {
        var parse = () => InstallerOptionsParser.Parse(new[] { key, value });

        parse.Should()
            .Throw<InstallerOptionsParserException>()
            .WithMessage("*interactive-only*");
    }
}
