using FluentAssertions;
using ProtoFleet.Installer.Core;
using Xunit;

namespace ProtoFleet.Installer.Tests;

public sealed class CommandEscapingTests
{
    [Fact]
    public void WindowsArgument_QuotesValuesWithWhitespace()
    {
        var escaped = CommandEscaping.WindowsArgument("Ubuntu 22.04");

        escaped.Should().Be("\"Ubuntu 22.04\"");
    }

    [Fact]
    public void BashSingleQuote_EscapesSingleQuotes()
    {
        var escaped = CommandEscaping.BashSingleQuote("alpha'beta");

        escaped.Should().Be("'alpha'\"'\"'beta'");
    }

    [Fact]
    public void ScheduledTaskCommandBuilder_QuotesDistroNamesWithSpaces()
    {
        var action = ScheduledTaskCommandBuilder.BuildTaskAction("Ubuntu Fleet");

        action.Should().Contain("-d \"Ubuntu Fleet\"");
        action.Should().Contain("bash -lc 'systemctl start docker || service docker start'");
    }
}
