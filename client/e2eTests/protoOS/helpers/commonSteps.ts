import { test } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { AuthPage } from "../pages/auth";

export class CommonSteps {
  constructor(private authPage: AuthPage) {}

  async authenticateAsAdmin() {
    await test.step("Authenticate as admin", async () => {
      await this.authPage.inputLoginPassword(testConfig.admin.password);
      await this.authPage.clickLoginButton();
      await this.authPage.validateToastMessage("You are now logged in as admin");
    });
  }

  async navigateToHome() {
    await test.step("Navigate to Home", async () => {
      await this.authPage.navigateToHome();
    });
  }

  async navigateToDiagnostics() {
    await test.step("Navigate to Diagnostics", async () => {
      await this.authPage.navigateToDiagnostics();
    });
  }

  async navigateToLogs() {
    await test.step("Navigate to Logs", async () => {
      await this.authPage.navigateToLogs();
    });
  }

  async navigateToAuthenticationSettings() {
    await test.step("Navigate to Authentication settings", async () => {
      await this.authPage.navigateToAuthenticationSettings();
    });
  }

  async navigateToGeneralSettings() {
    await test.step("Navigate to General settings", async () => {
      await this.authPage.navigateToGeneralSettings();
    });
  }

  async navigateToPoolsSettings() {
    await test.step("Navigate to Pools settings", async () => {
      await this.authPage.navigateToPoolsSettings();
    });
  }

  async navigateToHardwareSettings() {
    await test.step("Navigate to Hardware settings", async () => {
      await this.authPage.navigateToHardwareSettings();
    });
  }

  async navigateToCoolingSettings() {
    await test.step("Navigate to Cooling settings", async () => {
      await this.authPage.navigateToCoolingSettings();
    });
  }
}
