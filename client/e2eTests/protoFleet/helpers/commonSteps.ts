import { test } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";

export class CommonSteps {
  constructor(
    private authPage: AuthPage,
    private minersPage: MinersPage,
  ) {}

  async loginAsAdmin() {
    await test.step("Login as admin", async () => {
      await this.authPage.inputUsername(testConfig.users.admin.username);
      await this.authPage.inputPassword(testConfig.users.admin.password);
      await this.authPage.clickLogin();
      await this.authPage.validateLoggedIn();
    });
  }

  async goToMinersPage() {
    await test.step("Navigate to miners page", async () => {
      await this.minersPage.navigateToMinersPage();
      await this.minersPage.waitForMinersTitle();
      await this.minersPage.waitForMinersListToLoad();
    });
  }
}
