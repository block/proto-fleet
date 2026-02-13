using System.Windows;
using ProtoFleet.Installer.Core;
using ProtoFleet.Installer.Platform.Windows;

namespace ProtoFleet.Installer.App;

public partial class App : Application
{
    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);

        try
        {
            // The application manifest already requests elevation. This check is a safety guard.
            var elevation = new WindowsElevationService();
            if (!elevation.IsAdministrator())
            {
                MessageBox.Show(
                    "Administrator privileges are required to run this installer.",
                    "Proto Fleet Installer",
                    MessageBoxButton.OK,
                    MessageBoxImage.Warning);
                Shutdown();
                return;
            }

            var resumeStateStore = new JsonFileResumeStateStore();
            var resumeRegistrationService = new RunOnceResumeRegistrationService();

            InstallerOptions parsed;
            try
            {
                parsed = InstallerOptionsParser.Parse(e.Args);
            }
            catch (InstallerOptionsParserException parseError)
            {
                MessageBox.Show(
                    parseError.Message,
                    "Proto Fleet Installer - Invalid Arguments",
                    MessageBoxButton.OK,
                    MessageBoxImage.Error);
                Environment.ExitCode = (int)InstallerExitCode.Fatal;
                Shutdown();
                return;
            }

            if (parsed.AutoStart)
            {
                var resumeState = resumeStateStore.Load();
                if (resumeState?.Options is not null)
                {
                    parsed = resumeState.Options with { AutoStart = true };
                }
                else
                {
                    resumeStateStore.Clear();
                    resumeRegistrationService.ClearAutoResume();
                    parsed = parsed with { AutoStart = false };
                }
            }

            var mainWindow = new MainWindow(parsed, resumeStateStore, resumeRegistrationService);
            MainWindow = mainWindow;
            mainWindow.Show();
        }
        catch (Exception ex)
        {
            MessageBox.Show(
                $"Installer failed to start.{Environment.NewLine}{Environment.NewLine}{ex}",
                "Proto Fleet Installer - Startup Error",
                MessageBoxButton.OK,
                MessageBoxImage.Error);
            Environment.ExitCode = (int)InstallerExitCode.Fatal;
            Shutdown();
        }
    }
}
