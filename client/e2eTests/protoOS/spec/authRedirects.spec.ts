import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

const DEFAULT_PASSWORD = "FactoryPass123!";

test.describe("Authentication redirects", () => {
  test("redirects protected routes to onboarding authentication while the default password is still active", async ({
    page,
    authStateHelper,
    authenticationPage,
  }) => {
    await test.step("Seed the simulator with an active default password", async () => {
      await authStateHelper.setState({
        password: DEFAULT_PASSWORD,
        defaultPassword: DEFAULT_PASSWORD,
        onboarded: true,
      });
    });

    await test.step("Open a protected settings route", async () => {
      await page.goto("/settings/general");
    });

    await test.step("Validate the user is redirected to update the default password", async () => {
      await authenticationPage.validateOnboardingAuthenticationUrl();
      await authenticationPage.validateTitle("Update your admin login");
      await authenticationPage.validateTextIsVisible(
        "Your miner is still using the factory default password. Change it now to continue setup.",
      );
    });
  });

  test("routes dismissed login-modal flows to authentication settings", async ({
    page,
    authStateHelper,
    authenticationPage,
  }) => {
    await test.step("Seed the simulator with a non-default admin password", async () => {
      await authStateHelper.setState({
        password: testConfig.admin.password,
        defaultPassword: DEFAULT_PASSWORD,
        onboarded: true,
      });
    });

    await test.step("Open a protected settings route without an authenticated session", async () => {
      await page.goto("/settings/general");
    });

    await test.step("Dismiss the login modal", async () => {
      await authenticationPage.validateLoginRequiredModal();
      await authenticationPage.dismissLoginRequiredModal();
    });

    await test.step("Validate the user lands on the public authentication recovery page", async () => {
      await authenticationPage.validateSettingsAuthenticationUrl();
      await authenticationPage.validateTitle("Update your admin login");
      await authenticationPage.validateTextIsVisible(
        "Your admin login is used to modify performance settings or mining pool configurations for this miner.",
      );
    });
  });
});
