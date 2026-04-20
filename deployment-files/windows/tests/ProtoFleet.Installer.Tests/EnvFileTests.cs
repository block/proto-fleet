using FluentAssertions;
using ProtoFleet.Installer.Core;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class EnvFileTests
{
    [Fact]
    public void BuildGenerated_ShouldIncludeRequiredKeysAndValidSecrets()
    {
        var values = EnvFile.BuildGenerated();

        EnvFile.HasRequiredKeys(values, out var missing).Should().BeTrue();
        missing.Should().BeEmpty();

        EnvFile.ValidateSecrets(values, out var error).Should().BeTrue();
        error.Should().BeNull();
    }

    [Fact]
    public void ValidateSecrets_ShouldFailForShortAuthKey()
    {
        var values = EnvFile.BuildGenerated();
        values["AUTH_CLIENT_SECRET_KEY"] = "short";

        EnvFile.ValidateSecrets(values, out var error).Should().BeFalse();
        error.Should().Contain("AUTH_CLIENT_SECRET_KEY");
    }
}
