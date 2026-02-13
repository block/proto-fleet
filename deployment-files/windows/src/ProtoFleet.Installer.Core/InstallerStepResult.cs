namespace ProtoFleet.Installer.Core;

public sealed class InstallerStepResult
{
    public static InstallerStepResult Succeeded() => new() { Success = true };

    public static InstallerStepResult Failed(string message, InstallerExitCode code = InstallerExitCode.Fatal) =>
        new() { Success = false, ErrorMessage = message, ExitCode = code };

    public static InstallerStepResult AwaitUserAction(
        InstallerUserActionType actionType,
        string message,
        string? actionContext = null) =>
        new()
        {
            Success = false,
            RequiresUserAction = true,
            UserActionType = actionType,
            ErrorMessage = message,
            ActionContext = actionContext,
            ExitCode = InstallerExitCode.Success
        };

    public bool Success { get; init; }

    public string? ErrorMessage { get; init; }

    public InstallerExitCode ExitCode { get; init; } = InstallerExitCode.Success;

    public bool RequiresUserAction { get; init; }

    public InstallerUserActionType UserActionType { get; init; } = InstallerUserActionType.None;

    public string? ActionContext { get; init; }
}
