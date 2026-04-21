using System.IO;
using System.Text;
using System.Windows;
using System.Windows.Controls;
using System.Diagnostics;
using Microsoft.Win32;
using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Platform.Windows;

namespace ProtoFleet.Installer.App;

public partial class MainWindow : Window
{
    private enum InstallerRunMode
    {
        Full = 0,
        ResumeFromLinuxUserSetup = 1
    }

    private readonly InstallerOptions _seedOptions;
    private readonly IInstallerResumeStateStore _resumeStateStore;
    private readonly IResumeRegistrationService _resumeRegistrationService;
    private readonly InstallerApplicationService _installerApplicationService;
    private readonly LinuxUserSetupCoordinator _linuxUserSetupCoordinator;

    private InstallerOptions? _pendingOptions;
    private bool _isInstalling;
    private bool _hasWaitStep;
    private string? _waitingDistro;
    private CancellationTokenSource? _installCts;
    private string? _completionFleetUrl;
    private const int LinuxUserLaunchRetryLimit = 3;
    private static readonly TimeSpan WaitProbeTimeout = TimeSpan.FromSeconds(25);
    private static readonly TimeSpan WaitProbeInterval = TimeSpan.FromSeconds(8);
    private const string RunTitleReady = "Ready to Install";
    private const string RunTitleInstalling = "Installing";
    private const string RunTitleFailed = "Install Failed";

    public MainWindow(
        InstallerOptions seedOptions,
        IInstallerResumeStateStore resumeStateStore,
        IResumeRegistrationService resumeRegistrationService)
    {
        _seedOptions = seedOptions;
        _resumeStateStore = resumeStateStore;
        _resumeRegistrationService = resumeRegistrationService;
        _installerApplicationService = new InstallerApplicationService(AppendLogLine, ResolveLogPath);
        _linuxUserSetupCoordinator = new LinuxUserSetupCoordinator(
            executorFactory: _installerApplicationService.CreateWslExecutor,
            appendLogLine: AppendLogLine,
            pollInterval: WaitProbeInterval,
            probeTimeout: WaitProbeTimeout);
        _linuxUserSetupCoordinator.StatusChanged += message =>
            Dispatcher.Invoke(() => WaitStatusTextBlock.Text = message);
        _linuxUserSetupCoordinator.ResumeRequested += HandleLinuxUserResumeRequestedAsync;

        InitializeComponent();
        ApplySeedOptions();
        UpdateModeUi();
        ShowStep(0);

        if (_seedOptions.AutoStart)
        {
            _pendingOptions = _seedOptions with { AutoStart = false };
            InstallSummaryTextBlock.Text = BuildInstallSummary(_pendingOptions);
            ShowStep(2);
            AppendLogLine("[INFO] Resuming installer after reboot...");
            Dispatcher.BeginInvoke(new Action(() => _ = RunInstallAsync(_pendingOptions, InstallerRunMode.Full)));
        }
    }

    protected override void OnClosed(EventArgs e)
    {
        _installCts?.Cancel();
        _installCts?.Dispose();
        _installCts = null;
        _linuxUserSetupCoordinator.Dispose();
        base.OnClosed(e);
    }

    private void ApplySeedOptions()
    {
        DeploymentPathTextBox.Text = _seedOptions.DeploymentPath ?? string.Empty;
        TarballPathTextBox.Text = _seedOptions.TarballPath ?? string.Empty;
        ConfigFileTextBox.Text = _seedOptions.ConfigFilePath ?? string.Empty;
        InstallDirTextBox.Text = string.IsNullOrWhiteSpace(_seedOptions.InstallDir) ? "~/proto-fleet" : _seedOptions.InstallDir;
        CertPathTextBox.Text = _seedOptions.ExistingCertPath ?? string.Empty;
        KeyPathTextBox.Text = _seedOptions.ExistingKeyPath ?? string.Empty;
        EnableTaskCheckBox.IsChecked = _seedOptions.EnableAutoStartTask;
        var setupTag = _seedOptions.SetupMode == SetupMode.Guided ? "Advanced" : "Simple";
        SelectComboByTag(SetupModeCombo, setupTag);
        SelectComboByTag(ProtocolModeCombo, _seedOptions.ProtocolMode.ToString());
    }

    private void CancelButton_OnClick(object sender, RoutedEventArgs e)
    {
        if (_isInstalling)
        {
            CancelActiveRunAndClose();
            return;
        }

        ClearAutoResumeState();
        Environment.ExitCode = (int)InstallerExitCode.Success;
        Close();
    }

    private void WelcomeContinueButton_OnClick(object sender, RoutedEventArgs e)
    {
        ShowStep(1);
    }

    private void ConfigurationBackButton_OnClick(object sender, RoutedEventArgs e)
    {
        if (_isInstalling)
        {
            return;
        }

        ShowStep(0);
    }

    private void ConfigurationContinueButton_OnClick(object sender, RoutedEventArgs e)
    {
        if (_isInstalling)
        {
            return;
        }

        try
        {
            _pendingOptions = BuildOptionsFromUi();
        }
        catch (InvalidOperationException ex)
        {
            MessageBox.Show(ex.Message, "Configuration Required", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }

        InstallSummaryTextBlock.Text = BuildInstallSummary(_pendingOptions);
        SetRunTitleReady();
        ShowStep(2);
        AppendLogLine("[INFO] Review details, then start installation.");
    }

    private void RunBackButton_OnClick(object sender, RoutedEventArgs e)
    {
        if (_isInstalling)
        {
            return;
        }

        ShowStep(1);
    }

    private void SetupModeCombo_OnSelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        UpdateModeUi();
    }

    private void TarballBrowseButton_OnClick(object sender, RoutedEventArgs e)
    {
        BrowseForFile(
            TarballPathTextBox,
            "Select deployment tarball",
            "Tar archives (*.tar;*.tar.gz;*.tgz)|*.tar;*.tar.gz;*.tgz|All files (*.*)|*.*");
    }

    private void ConfigFileBrowseButton_OnClick(object sender, RoutedEventArgs e)
    {
        BrowseForFile(
            ConfigFileTextBox,
            "Select config file",
            "Config files (*.env;*.yaml;*.yml)|*.env;*.yaml;*.yml|All files (*.*)|*.*");
    }

    private void CertPathBrowseButton_OnClick(object sender, RoutedEventArgs e)
    {
        BrowseForFile(
            CertPathTextBox,
            "Select certificate file",
            "Certificate files (*.crt;*.cer;*.pem)|*.crt;*.cer;*.pem|All files (*.*)|*.*");
    }

    private void KeyPathBrowseButton_OnClick(object sender, RoutedEventArgs e)
    {
        BrowseForFile(
            KeyPathTextBox,
            "Select private key file",
            "Key files (*.key;*.pem)|*.key;*.pem|All files (*.*)|*.*");
    }

    private void BrowseForFile(TextBox target, string title, string filter)
    {
        var dialog = new OpenFileDialog
        {
            Title = title,
            Filter = filter,
            CheckFileExists = true,
            Multiselect = false
        };

        var currentValue = target.Text?.Trim();
        if (!string.IsNullOrWhiteSpace(currentValue))
        {
            try
            {
                if (File.Exists(currentValue))
                {
                    dialog.InitialDirectory = Path.GetDirectoryName(currentValue);
                    dialog.FileName = Path.GetFileName(currentValue);
                }
                else if (Directory.Exists(currentValue))
                {
                    dialog.InitialDirectory = currentValue;
                }
            }
            catch
            {
                // Ignore invalid paths and fall back to dialog defaults.
            }
        }

        if (dialog.ShowDialog(this) == true)
        {
            target.Text = dialog.FileName;
        }
    }

    private async void StartInstallButton_OnClick(object sender, RoutedEventArgs e)
    {
        var options = _pendingOptions ?? BuildOptionsFromUi();
        await RunInstallAsync(options, InstallerRunMode.Full);
    }

    private async Task RunInstallAsync(
        InstallerOptions options,
        InstallerRunMode runMode,
        string? resumeDistro = null)
    {
        var runFailed = false;
        var runCancelled = false;
        _installCts?.Dispose();
        _installCts = new CancellationTokenSource();
        var runToken = _installCts.Token;

        try
        {
            SetRunningState(isRunning: true);
            ShowStep(2);
            SetRunTitleInstalling();
            if (runMode == InstallerRunMode.Full)
            {
                LogTextBox.Clear();
                AppendLogLine("[INFO] Installer is running...");
            }
            else
            {
                AppendLogLine($"[INFO] Resuming from checkpoint: {InstallerCheckpoint.LinuxUserProvisioning}");
            }
            InstallerRunResult result;
            if (runMode == InstallerRunMode.ResumeFromLinuxUserSetup)
            {
                var resumedDistro = string.IsNullOrWhiteSpace(resumeDistro) ? _waitingDistro : resumeDistro;
                if (string.IsNullOrWhiteSpace(resumedDistro))
                {
                    runFailed = true;
                    AppendLogLine("[ERROR] Could not resume from Linux user setup because distro was not available.");
                    Environment.ExitCode = (int)InstallerExitCode.Fatal;
                    return;
                }

                result = await _installerApplicationService.ResumeFromLinuxUserSetupAsync(options, resumedDistro, runToken);
            }
            else
            {
                result = await _installerApplicationService.RunAsync(options, runToken);
            }

            if (result.RequiresUserAction && result.UserActionType == InstallerUserActionType.WaitForLinuxUserSetup)
            {
                var distro = !string.IsNullOrWhiteSpace(result.ActionContext)
                    ? result.ActionContext
                    : (result.SelectedDistro ?? "Ubuntu");
                BeginLinuxUserWait(distro!, options, result.ErrorMessage);
                Environment.ExitCode = (int)InstallerExitCode.Success;
                return;
            }

            switch (result.ExitCode)
            {
                case InstallerExitCode.Success:
                    ClearAutoResumeState();
                    ShowCompletionPage(options, result);
                    AppendLogLine("[INFO] Installation completed. Review details on the completion page.");
                    break;
                case InstallerExitCode.RebootRequired:
                    PersistAutoResumeState(options, result.Checkpoint, result.SelectedDistro);
                    var rebootMessage =
                        "A reboot is required before installation can continue.\n\n" +
                        "The installer is configured to resume automatically after reboot.\n\n" +
                        "Reboot now?";
                    AppendLogLine(
                        "[WARN] A reboot is required before installation can continue. " +
                        "Installer auto-resume has been configured.");
                    var rebootChoice = MessageBox.Show(
                        rebootMessage,
                        "Reboot Required",
                        MessageBoxButton.YesNo,
                        MessageBoxImage.Warning);
                    if (rebootChoice == MessageBoxResult.Yes)
                    {
                        try
                        {
                            AppendLogLine("[INFO] User accepted reboot prompt. Initiating immediate restart.");
                            Process.Start(new ProcessStartInfo
                            {
                                FileName = "shutdown.exe",
                                Arguments = "/r /t 0",
                                UseShellExecute = true,
                                Verb = "runas"
                            });
                            Close();
                            return;
                        }
                        catch (Exception ex)
                        {
                            AppendLogLine($"[WARN] Failed to initiate reboot automatically: {ex.Message}");
                            AppendLogLine(
                                "[WARN] Reboot is still required. " +
                                "Please restart Windows manually; installer resume is already configured.");
                        }
                    }
                    else
                    {
                        AppendLogLine(
                            "[INFO] Reboot deferred. Restart Windows when ready; installer resume is already configured.");
                    }
                    break;
                case InstallerExitCode.InvalidDeploymentInput:
                    ClearAutoResumeState();
                    AppendLogLine($"[ERROR] Deployment input is invalid. {result.ErrorMessage}");
                    MessageBox.Show(
                        $"Deployment input is invalid.{Environment.NewLine}{result.ErrorMessage}",
                        "Invalid Deployment Input",
                        MessageBoxButton.OK,
                        MessageBoxImage.Error);
                    runFailed = true;
                    break;
                case InstallerExitCode.Cancelled:
                    runCancelled = true;
                    AppendLogLine("[INFO] Installation was cancelled.");
                    break;
                default:
                    ClearAutoResumeState();
                    AppendLogLine($"[ERROR] Install failed. {result.ErrorMessage}");
                    MessageBox.Show(
                        $"Install failed.{Environment.NewLine}{result.ErrorMessage}",
                        "Install Failed",
                        MessageBoxButton.OK,
                        MessageBoxImage.Error);
                    runFailed = true;
                    break;
            }

            Environment.ExitCode = (int)result.ExitCode;
        }
        catch (OperationCanceledException) when (runToken.IsCancellationRequested)
        {
            runCancelled = true;
            ClearAutoResumeState();
            _linuxUserSetupCoordinator.Cancel();
            AppendLogLine("[INFO] Installation cancelled by user.");
            Environment.ExitCode = (int)InstallerExitCode.Cancelled;
        }
        catch (Exception ex)
        {
            ClearAutoResumeState();
            AppendLogLine($"[ERROR] Unhandled exception: {ex}");
            Environment.ExitCode = (int)InstallerExitCode.Fatal;
            runFailed = true;
        }
        finally
        {
            SetRunningState(isRunning: false);
            if (RunStepPanel.Visibility == Visibility.Visible)
            {
                if (runFailed)
                {
                    SetRunTitleFailed();
                }
                else if (runCancelled)
                {
                    SetRunTitleReady();
                }
                else
                {
                    SetRunTitleReady();
                }
            }
        }
    }

    private void BeginLinuxUserWait(string distro, InstallerOptions options, string? message)
    {
        _waitingDistro = distro;
        _pendingOptions = options;
        _hasWaitStep = true;

        WaitDistroNameTextBlock.Text = distro;
        WaitStatusTextBlock.Text = message ?? "Waiting for Linux user creation in distro setup...";
        ShowStep(3);
        _linuxUserSetupCoordinator.BeginWait(
            distro,
            options,
            message ?? "Waiting for Linux user creation in distro setup...",
            LinuxUserLaunchRetryLimit);
    }

    private async Task HandleLinuxUserResumeRequestedAsync(string distro, InstallerOptions options)
    {
        await Dispatcher.InvokeAsync(() =>
        {
            WaitStatusTextBlock.Text = "Linux user setup detected. Resuming installer...";
            _ = RunInstallAsync(options, InstallerRunMode.ResumeFromLinuxUserSetup, distro);
        });
    }

    private async void WaitOpenUbuntuButton_OnClick(object sender, RoutedEventArgs e)
    {
        await _linuxUserSetupCoordinator.OpenUbuntuWindowAsync(LinuxUserLaunchRetryLimit);
    }

    private async void WaitCompletedButton_OnClick(object sender, RoutedEventArgs e)
    {
        await _linuxUserSetupCoordinator.CheckNowAsync();
    }

    private void WaitCopyCommandButton_OnClick(object sender, RoutedEventArgs e)
    {
        var command = _linuxUserSetupCoordinator.GetManualCommand();
        if (string.IsNullOrWhiteSpace(command))
        {
            return;
        }

        try
        {
            Clipboard.SetText(command);
            WaitStatusTextBlock.Text = $"Copied command to clipboard: {command}";
        }
        catch (Exception ex)
        {
            WaitStatusTextBlock.Text = $"Could not copy command. Run manually: {command}. ({ex.Message})";
        }
    }

    private void WaitCancelButton_OnClick(object sender, RoutedEventArgs e)
    {
        CancelActiveRunAndClose();
    }

    private void OpenFleetButton_OnClick(object sender, RoutedEventArgs e)
    {
        if (string.IsNullOrWhiteSpace(_completionFleetUrl))
        {
            AppendLogLine("[WARN] Fleet URL is not available.");
            return;
        }

        try
        {
            Process.Start(new ProcessStartInfo
            {
                FileName = _completionFleetUrl,
                UseShellExecute = true
            });
            AppendLogLine($"[INFO] Opened Fleet URL: {_completionFleetUrl}");
        }
        catch (Exception ex)
        {
            AppendLogLine($"[WARN] Could not open browser automatically. Navigate to {_completionFleetUrl}.");
            AppendLogLine($"[WARN] Failed to open Fleet URL '{_completionFleetUrl}': {ex.Message}");
        }
    }

    private void CompletionUrlHyperlink_OnClick(object sender, RoutedEventArgs e)
    {
        OpenFleetButton_OnClick(sender, e);
    }

    private void CompletionRunWslHyperlink_OnClick(object sender, RoutedEventArgs e)
    {
        try
        {
            Process.Start(new ProcessStartInfo
            {
                FileName = "wsl",
                UseShellExecute = true
            });

            AppendLogLine("[INFO] Started WSL.");
        }
        catch (Exception ex)
        {
            try
            {
                Clipboard.SetText("wsl");
                AppendLogLine("[WARN] Could not start WSL automatically. Command copied: wsl");
            }
            catch
            {
                AppendLogLine("[WARN] Could not start WSL automatically. Run command: wsl");
            }

            AppendLogLine($"[WARN] Failed to start WSL from completion page: {ex.Message}");
        }
    }

    private void CopyFleetUrlButton_OnClick(object sender, RoutedEventArgs e)
    {
        if (string.IsNullOrWhiteSpace(_completionFleetUrl))
        {
            AppendLogLine("[WARN] Fleet URL is not available.");
            return;
        }

        try
        {
            Clipboard.SetText(_completionFleetUrl);
            AppendLogLine($"[INFO] Copied Fleet URL: {_completionFleetUrl}");
        }
        catch (Exception ex)
        {
            AppendLogLine($"[WARN] Could not copy Fleet URL. {_completionFleetUrl}");
            AppendLogLine($"[WARN] Failed to copy Fleet URL '{_completionFleetUrl}': {ex.Message}");
        }
    }

    private void FinishButton_OnClick(object sender, RoutedEventArgs e)
    {
        _linuxUserSetupCoordinator.Cancel();
        ClearAutoResumeState();
        Environment.ExitCode = (int)InstallerExitCode.Success;
        Close();
    }

    private void CancelActiveRunAndClose()
    {
        _linuxUserSetupCoordinator.Cancel();
        _installCts?.Cancel();
        ClearAutoResumeState();
        Environment.ExitCode = (int)InstallerExitCode.Cancelled;
        Close();
    }

    private void PersistAutoResumeState(InstallerOptions options, InstallerCheckpoint checkpoint, string? selectedDistro)
    {
        try
        {
            var resumableOptions = options with { AutoStart = true };
            _resumeStateStore.Save(new InstallerResumeState
            {
                Options = resumableOptions,
                Checkpoint = checkpoint,
                SelectedDistro = selectedDistro,
                CreatedAtUtc = DateTimeOffset.UtcNow,
            });

            var executablePath = Environment.ProcessPath;
            if (!string.IsNullOrWhiteSpace(executablePath))
            {
                _resumeRegistrationService.RegisterAutoResume(executablePath);
            }
        }
        catch (Exception ex)
        {
            AppendLogLine($"[WARN] Failed to configure auto-resume state: {ex.Message}");
        }
    }

    private void ClearAutoResumeState()
    {
        try
        {
            _resumeStateStore.Clear();
            _resumeRegistrationService.ClearAutoResume();
        }
        catch
        {
            // no-op
        }
    }

    private InstallerOptions BuildOptionsFromUi()
    {
        var modeTag = (SetupModeCombo.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "Simple";
        var protocolTag = (ProtocolModeCombo.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "Auto";
        var setupMode = modeTag.Equals("Advanced", StringComparison.OrdinalIgnoreCase) ? SetupMode.Guided : SetupMode.Simple;
        var protocol = protocolTag switch
        {
            "Http" => ProtocolMode.Http,
            "HttpsSelfSigned" => ProtocolMode.HttpsSelfSigned,
            "HttpsExisting" => ProtocolMode.HttpsExisting,
            _ => ProtocolMode.Auto
        };

        var guided = setupMode == SetupMode.Guided;

        return new InstallerOptions
        {
            VersionLabel = _seedOptions.VersionLabel,
            DeploymentPath = guided ? NullIfEmpty(DeploymentPathTextBox.Text) : _seedOptions.DeploymentPath,
            TarballPath = guided ? NullIfEmpty(TarballPathTextBox.Text) : _seedOptions.TarballPath,
            ConfigFilePath = guided ? NullIfEmpty(ConfigFileTextBox.Text) : _seedOptions.ConfigFilePath,
            InstallDir = guided
                ? (string.IsNullOrWhiteSpace(InstallDirTextBox.Text) ? "~/proto-fleet" : InstallDirTextBox.Text.Trim())
                : (string.IsNullOrWhiteSpace(_seedOptions.InstallDir) ? "~/proto-fleet" : _seedOptions.InstallDir),
            SetupMode = setupMode,
            ProtocolMode = setupMode == SetupMode.Simple ? ProtocolMode.Auto : protocol,
            ExistingCertPath = guided ? NullIfEmpty(CertPathTextBox.Text) : _seedOptions.ExistingCertPath,
            ExistingKeyPath = guided ? NullIfEmpty(KeyPathTextBox.Text) : _seedOptions.ExistingKeyPath,
            EnableAutoStartTask = EnableTaskCheckBox.IsChecked == true,
            AutoStart = false,
            Debug = _seedOptions.Debug,
        };
    }

    private void UpdateModeUi()
    {
        if (SetupModeCombo is null || GuidedOptionsPanel is null || ModeDescriptionTextBlock is null)
        {
            return;
        }

        var modeTag = (SetupModeCombo.SelectedItem as ComboBoxItem)?.Tag?.ToString() ?? "Simple";
        var guided = modeTag.Equals("Advanced", StringComparison.OrdinalIgnoreCase);
        GuidedOptionsPanel.Visibility = guided ? Visibility.Visible : Visibility.Collapsed;
        ModeDescriptionTextBlock.Text = guided
            ? "Advanced mode exposes protocol and deployment inputs for custom scenarios."
            : "Simple mode keeps setup on-rails with defaults: auto-detect deployment, interactive Ubuntu first-run user setup, and HTTP configuration.";
    }

    private static string BuildInstallSummary(InstallerOptions options)
    {
        var builder = new StringBuilder();
        var setupModeLabel = options.SetupMode == SetupMode.Guided ? "Advanced" : "Simple";
        builder.AppendLine($"Setup mode: {setupModeLabel}");
        builder.AppendLine($"Install directory: {options.InstallDir}");
        builder.AppendLine($"Auto-start task: {(options.EnableAutoStartTask ? "Enabled" : "Disabled")}");

        if (options.SetupMode == SetupMode.Simple)
        {
            builder.AppendLine("Protocol: HTTP (simple mode default)");
            builder.AppendLine("Deployment source: automatic discovery, then transfer into WSL via tarball copy");
        }
        else
        {
            builder.AppendLine($"Protocol: {options.ProtocolMode}");
            builder.AppendLine($"Deployment path: {ConfiguredOrDefault(options.DeploymentPath, "Auto-detect from known install locations")}");
            builder.AppendLine($"Tarball path: {ConfiguredOrDefault(options.TarballPath, "Auto-create and transfer tarball into WSL")}");
            builder.AppendLine($"Config file: {ConfiguredOrDefault(options.ConfigFilePath, "No external .env override")}");
            if (options.ProtocolMode == ProtocolMode.HttpsExisting)
            {
                builder.AppendLine($"TLS cert path: {ConfiguredOrDefault(options.ExistingCertPath, "Required for HTTPS existing mode (not provided)")}");
                builder.AppendLine($"TLS key path: {ConfiguredOrDefault(options.ExistingKeyPath, "Required for HTTPS existing mode (not provided)")}");
            }
            else if (options.ProtocolMode == ProtocolMode.HttpsSelfSigned)
            {
                builder.AppendLine("TLS cert/key: Will be auto-generated (self-signed)");
            }
            else if (options.ProtocolMode == ProtocolMode.Http)
            {
                builder.AppendLine("TLS cert/key: Not used for selected protocol");
            }
            else
            {
                builder.AppendLine("TLS cert/key: Auto-managed based on selected protocol");
            }
        }

        return builder.ToString().TrimEnd();
    }

    private static string ConfiguredOrDefault(string? value, string defaultText)
    {
        return string.IsNullOrWhiteSpace(value) ? defaultText : value.Trim();
    }

    private void ShowCompletionPage(InstallerOptions options, InstallerRunResult runResult)
    {
        var protocol = options.SetupMode == SetupMode.Simple || options.ProtocolMode == ProtocolMode.Http ? "http" : "https";
        _completionFleetUrl = $"{protocol}://localhost";

        CompletionHeaderTextBox.Text = "Proto Fleet Installation Complete";
        CompletionSummaryTextBox.Text = BuildCompletionSummary(options, runResult);
        CompletionUrlHyperlinkRun.Text = _completionFleetUrl;
        CompletionCommandsTextBox.Text = BuildCompletionCommands(options, runResult);

        ShowStep(CompletionStepIndex);
    }

    private static string BuildCompletionSummary(InstallerOptions options, InstallerRunResult runResult)
    {
        var builder = new StringBuilder();
        builder.AppendLine("Proto Fleet installation completed successfully.");
        builder.AppendLine($"Setup mode: {(options.SetupMode == SetupMode.Guided ? "Advanced" : "Simple")}");
        builder.AppendLine($"Auto-start task: {(options.EnableAutoStartTask ? "Enabled" : "Disabled")}");

        if (!string.IsNullOrWhiteSpace(runResult.LinuxProvisionedUsername))
        {
            builder.AppendLine($"Linux user: {runResult.LinuxProvisionedUsername}");
        }

        if (runResult.Warnings.Count > 0)
        {
            builder.AppendLine("Warnings:");
            foreach (var warning in runResult.Warnings)
            {
                builder.AppendLine($"  - {warning}");
            }
        }

        return builder.ToString().TrimEnd();
    }

    private static string BuildCompletionCommands(InstallerOptions options, InstallerRunResult runResult)
    {
        var distro = string.IsNullOrWhiteSpace(runResult.SelectedDistro) ? "Ubuntu" : runResult.SelectedDistro.Trim();
        var installDir = string.IsNullOrWhiteSpace(options.InstallDir) ? "~/proto-fleet" : options.InstallDir.Trim();

        var distroArg = CommandEscaping.PowerShellSingleQuote(distro);
        var installDirBashArg = CommandEscaping.BashSingleQuote(installDir);
        string ComposeCommand(string dockerComposeArgs) =>
            $"wsl -d {distroArg} -- bash -lc \"cd {installDirBashArg} && docker compose {dockerComposeArgs}\"";

        return string.Join(Environment.NewLine, new[]
        {
            "# Initialize WSL if it is not running yet",
            "wsl",
            string.Empty,
            "# Show Fleet container status",
            ComposeCommand("ps"),
            string.Empty,
            "# Stream Fleet logs",
            ComposeCommand("logs -f"),
            string.Empty,
            "# Restart Fleet containers",
            ComposeCommand("down"),
            ComposeCommand("up -d")
        });
    }

    private void SetRunTitleReady()
    {
        RunStepTitleTextBlock.Text = RunTitleReady;
    }

    private void SetRunTitleInstalling()
    {
        RunStepTitleTextBlock.Text = RunTitleInstalling;
    }

    private void SetRunTitleFailed()
    {
        RunStepTitleTextBlock.Text = RunTitleFailed;
    }

    private void SetRunningState(bool isRunning)
    {
        _isInstalling = isRunning;
        StartInstallButton.IsEnabled = !isRunning;
        RunBackButton.IsEnabled = !isRunning;
        InstallProgressBar.IsIndeterminate = isRunning;
    }

    private void ShowStep(int stepIndex)
    {
        var completionStepIndex = CompletionStepIndex;
        WelcomeStepPanel.Visibility = stepIndex == 0 ? Visibility.Visible : Visibility.Collapsed;
        ConfigurationStepPanel.Visibility = stepIndex == 1 ? Visibility.Visible : Visibility.Collapsed;
        RunStepPanel.Visibility = stepIndex == 2 ? Visibility.Visible : Visibility.Collapsed;
        WaitForLinuxUserStepPanel.Visibility = _hasWaitStep && stepIndex == 3 ? Visibility.Visible : Visibility.Collapsed;
        CompletionStepPanel.Visibility = stepIndex == completionStepIndex ? Visibility.Visible : Visibility.Collapsed;

        var total = _hasWaitStep ? 5 : 4;
        StepIndicatorText.Text = $"Step {Math.Min(stepIndex + 1, total)} of {total}";
    }

    private int CompletionStepIndex => _hasWaitStep ? 4 : 3;

    private void AppendLogLine(string message)
    {
        Dispatcher.Invoke(() =>
        {
            LogTextBox.AppendText(message + Environment.NewLine);
            LogTextBox.ScrollToEnd();
        });
    }

    private static string ResolveLogPath()
    {
        try
        {
            return Path.Combine(AppContext.BaseDirectory, "fleet-exe.log");
        }
        catch
        {
            var fallback = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData),
                "ProtoFleet",
                "logs",
                "fleet-exe.log");
            var directory = Path.GetDirectoryName(fallback);
            if (!string.IsNullOrWhiteSpace(directory))
            {
                Directory.CreateDirectory(directory);
            }

            return fallback;
        }
    }

    private static void SelectComboByTag(ComboBox comboBox, string tag)
    {
        foreach (var item in comboBox.Items.OfType<ComboBoxItem>())
        {
            if (item.Tag?.ToString()?.Equals(tag, StringComparison.OrdinalIgnoreCase) == true)
            {
                comboBox.SelectedItem = item;
                return;
            }
        }
    }

    private static string? NullIfEmpty(string? value)
    {
        return string.IsNullOrWhiteSpace(value) ? null : value.Trim();
    }
}
