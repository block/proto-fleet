import { test } from "@playwright/test";
import { testConfig } from "../config/test.config";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsPage } from "../pages/settings";
import { SettingsTeamPage } from "../pages/settingsTeam";

type CreateRoleAndMemberOptions = {
  roleName: string;
  roleDescription: string;
  permissionKeys: string[];
  username: string;
};

type FirstLoginOptions = {
  username: string;
  temporaryPassword: string;
  newPassword: string;
};

export class CommonSteps {
  constructor(
    private authPage: AuthPage,
    private minersPage: MinersPage,
    private settingsPage?: SettingsPage,
    private settingsTeamPage?: SettingsTeamPage,
  ) {}

  private requireTeamHelpers() {
    if (!this.settingsPage || !this.settingsTeamPage) {
      throw new Error("Settings helpers are required for team and role setup flows.");
    }

    return {
      settingsPage: this.settingsPage,
      settingsTeamPage: this.settingsTeamPage,
    };
  }

  async loginAsAdmin({ forceReauth = false }: { forceReauth?: boolean } = {}) {
    await test.step("Login as admin", async () => {
      // eslint-disable-next-line playwright/no-conditional-in-test
      if (forceReauth && (await this.authPage.isAlreadyLoggedIn())) {
        await this.authPage.logout();
        await this.authPage.validateRedirectedToAuth();
      }

      // eslint-disable-next-line playwright/no-conditional-in-test
      if (await this.authPage.isAlreadyLoggedIn()) {
        return;
      }
      await this.authPage.inputUsername(testConfig.users.admin.username);
      await this.authPage.inputPassword(testConfig.users.admin.password);
      await this.authPage.clickLogin();
      await this.authPage.validateLoggedIn();
    });
  }

  async createRoleAndTeamMember({
    roleName,
    roleDescription,
    permissionKeys,
    username,
  }: CreateRoleAndMemberOptions): Promise<string> {
    const { settingsPage, settingsTeamPage } = this.requireTeamHelpers();

    return await test.step("Create a custom role and team member as admin", async () => {
      await this.loginAsAdmin({ forceReauth: true });
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.openRolesTab();
      await settingsTeamPage.createCustomRole(roleName, roleDescription, permissionKeys);
      await settingsTeamPage.openMembersTab();
      return await settingsTeamPage.createTeamMemberAndGetTemporaryPassword(username, roleName);
    });
  }

  async completeFirstLoginAsTeamMember({ username, temporaryPassword, newPassword }: FirstLoginOptions) {
    await test.step("Log in as the new team member and set a permanent password", async () => {
      // eslint-disable-next-line playwright/no-conditional-in-test
      if (await this.authPage.isAlreadyLoggedIn()) {
        await this.authPage.logout();
        await this.authPage.validateRedirectedToAuth();
      }
      await this.authPage.inputUsername(username);
      await this.authPage.inputPassword(temporaryPassword);
      await this.authPage.clickLogin();
      await this.authPage.validateUpdatePasswordTitle();
      await this.authPage.inputNewPassword(newPassword);
      await this.authPage.inputConfirmPassword(newPassword);
      await this.authPage.clickContinue();
      await this.authPage.clickLoginButton();
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
