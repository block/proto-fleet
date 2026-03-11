using ProtoFleet.Installer.Core;
using System.Text.RegularExpressions;
using System.Diagnostics;
using System.Text;

namespace ProtoFleet.Installer.Platform.Wsl;

public sealed class WslCommandExecutor
{
    private static readonly TimeSpan DefaultWslTimeout = TimeSpan.FromMinutes(10);
    private readonly ICommandRunner _commandRunner;
    private readonly ILogSink _logSink;

    public WslCommandExecutor(ICommandRunner commandRunner, ILogSink logSink)
    {
        _commandRunner = commandRunner;
        _logSink = logSink;
    }

    public async Task<CommandResult> RunWslAsync(
        string arguments,
        CancellationToken cancellationToken,
        TimeSpan? timeout = null)
    {
        _logSink.Info($"wsl.exe {arguments}");
        Task<CommandResult> commandTask;
        try
        {
            commandTask = _commandRunner.RunAsync(new CommandRequest
            {
                FileName = "wsl.exe",
                Arguments = arguments,
                Timeout = timeout ?? DefaultWslTimeout,
                // WSL command output is UTF-8 when redirected; forcing UTF-16 causes mojibake.
                StandardOutputEncoding = Encoding.UTF8,
                StandardErrorEncoding = Encoding.UTF8,
            }, cancellationToken);
        }
        catch (Exception ex)
        {
            _logSink.Error($"Failed to start wsl.exe {arguments}: {ex.Message}");
            return new CommandResult
            {
                ExitCode = -1,
                StandardError = ex.Message,
            };
        }
        var heartbeatInterval = ResolveHeartbeatInterval(arguments);
        while (!commandTask.IsCompleted)
        {
            await Task.Delay(heartbeatInterval, cancellationToken);
            if (!commandTask.IsCompleted)
            {
                _logSink.Info($"wsl.exe command still running: {arguments}");
            }
        }

        CommandResult result;
        try
        {
            result = await commandTask;
        }
        catch (TimeoutException ex)
        {
            _logSink.Error($"wsl.exe command timed out: {arguments}. {ex.Message}");
            result = new CommandResult
            {
                ExitCode = -2,
                StandardError = ex.Message,
            };
        }
        catch (Exception ex)
        {
            _logSink.Error($"wsl.exe command threw exception: {arguments}. {ex.Message}");
            result = new CommandResult
            {
                ExitCode = -3,
                StandardError = ex.Message,
            };
        }

        // Some older Windows 10 WSL builds emit UTF-16 LE even though we request UTF-8.
        // When decoded as UTF-8 each ASCII character is followed by a null byte ('\0').
        // Strip null bytes so downstream pattern matching (Contains, etc.) works correctly.
        result = StripNullBytes(result);

        if (!result.IsSuccess)
        {
            _logSink.Error(
                $"wsl.exe command failed (exit {result.ExitCode}): {arguments}. " +
                $"stderr={FormatSnippet(result.StandardError)} stdout={FormatSnippet(result.StandardOutput)}");
        }
        else if (!string.IsNullOrWhiteSpace(result.StandardError))
        {
            _logSink.Warn(
                $"wsl.exe command emitted stderr (exit 0): {arguments}. " +
                $"stderr={FormatSnippet(result.StandardError)}");
        }

        return result;
    }

    public Task<CommandResult> RunInDistroAsync(
        string distro,
        string bashCommand,
        bool asRoot,
        CancellationToken cancellationToken,
        TimeSpan? timeout = null)
    {
        var normalizedDistro = NormalizeDistroName(distro);
        if (!normalizedDistro.Equals(distro, StringComparison.Ordinal))
        {
            _logSink.Warn($"Normalized distro name from '{distro}' to '{normalizedDistro}' for command execution.");
        }

        var rootArg = asRoot ? "-u root " : string.Empty;
        var command = $"bash -lc {ShellEscaping.BashSingleQuote(bashCommand)}";
        var args = $"-d {CommandEscaping.WindowsArgument(normalizedDistro)} {rootArg}-- {command}";
        return RunWslAsync(args, cancellationToken, timeout);
    }

    public bool LaunchInteractiveDistro(string distro)
    {
        var normalizedDistro = NormalizeDistroName(distro);
        if (!normalizedDistro.Equals(distro, StringComparison.Ordinal))
        {
            _logSink.Warn($"Normalized distro name from '{distro}' to '{normalizedDistro}' for interactive launch.");
        }

        var quotedDistro = CommandEscaping.WindowsArgument(normalizedDistro);
        var attempts = new (string FileName, string Arguments)[]
        {
            ("wt.exe", $"wsl.exe -d {quotedDistro}"),
            ("cmd.exe", $"/c start \"Proto Fleet Ubuntu Setup\" wsl.exe -d {quotedDistro}"),
            ("wsl.exe", $"-d {quotedDistro}"),
        };

        foreach (var attempt in attempts)
        {
            if (TryStartVisibleProcess(attempt.FileName, attempt.Arguments))
            {
                _logSink.Info($"Launched interactive first-run for distro '{distro}' using {attempt.FileName}.");
                return true;
            }
        }

        _logSink.Warn($"Failed to launch interactive distro window for '{distro}' with all launch strategies.");
        return false;
    }

    private bool TryStartVisibleProcess(string fileName, string arguments)
    {
        try
        {
            var process = Process.Start(new ProcessStartInfo
            {
                FileName = fileName,
                Arguments = arguments,
                UseShellExecute = true,
                WindowStyle = ProcessWindowStyle.Normal
            });

            return process is not null;
        }
        catch (Exception ex)
        {
            _logSink.Warn($"Launch attempt failed: {fileName} {arguments}. {ex.Message}");
            return false;
        }
    }

    private static CommandResult StripNullBytes(CommandResult result)
    {
        var stdout = result.StandardOutput.Replace("\0", string.Empty);
        var stderr = result.StandardError.Replace("\0", string.Empty);
        if (stdout.Length == result.StandardOutput.Length && stderr.Length == result.StandardError.Length)
        {
            return result;
        }

        return new CommandResult
        {
            ExitCode = result.ExitCode,
            StandardOutput = stdout,
            StandardError = stderr,
            Duration = result.Duration,
        };
    }

    private static string FormatSnippet(string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return "<empty>";
        }

        var flattened = value.Replace('\r', ' ').Replace('\n', ' ').Trim();
        flattened = CollapseSpacedWords(flattened);
        return flattened.Length <= 500 ? flattened : $"{flattened[..500]}...";
    }

    private static string CollapseSpacedWords(string text)
    {
        // Some WSL outputs arrive as "D o w n l o a d i n g" due to encoding quirks.
        var cleaned = text.Replace('\0', ' ');
        cleaned = Regex.Replace(cleaned, @"\b(?:[A-Za-z0-9]\s+){3,}[A-Za-z0-9]\b", m => m.Value.Replace(" ", string.Empty));

        var tokens = cleaned.Split(' ', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);
        if (tokens.Length >= 4 && tokens.All(t => t.Length == 1 && char.IsLetterOrDigit(t[0])))
        {
            return string.Concat(tokens);
        }

        return cleaned;
    }

    private static string NormalizeDistroName(string distro)
    {
        if (string.IsNullOrWhiteSpace(distro))
        {
            return distro;
        }

        var collapsed = CollapseSpacedWords(distro).Trim();
        return Regex.Replace(collapsed, @"\s{2,}", " ");
    }

    private static TimeSpan ResolveHeartbeatInterval(string arguments)
    {
        if (arguments.Contains("--install", StringComparison.OrdinalIgnoreCase) ||
            arguments.Contains("--update", StringComparison.OrdinalIgnoreCase) ||
            arguments.Contains("--set-version", StringComparison.OrdinalIgnoreCase))
        {
            return TimeSpan.FromSeconds(20);
        }

        return TimeSpan.FromSeconds(5);
    }
}
