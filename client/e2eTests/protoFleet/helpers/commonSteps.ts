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
      // The setup project captures the admin session into storageState, so most
      // tests start already authenticated. Skip the login flow when the auth
      // form isn't on screen.
      // eslint-disable-next-line playwright/no-conditional-in-test
      if (!(await this.authPage.isLoginFormVisible())) {
        return;
      }
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
