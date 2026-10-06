import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class AuthenticationPage extends BasePage {
  private settingsForm() {
    return this.page.getByTestId("authentication-settings-form");
  }

  async validateUsernameFieldDisabledWithValue(expectedValue: string) {
    const usernameField = this.settingsForm().locator('input[id="username"]');
    await expect(usernameField).toBeDisabled();
    await expect(usernameField).toHaveValue(expectedValue);
  }

  async inputCurrentPassword(password: string) {
    await this.settingsForm().locator('input[id="currentPassword"]').fill(password);
  }

  async inputNewPassword(password: string) {
    await this.settingsForm().locator('input[id="password"]').fill(password);
  }

  async inputConfirmPassword(password: string) {
    await this.settingsForm().locator('input[id="confirmPassword"]').fill(password);
  }

  async clickContinue() {
    await this.settingsForm().getByRole("button", { name: "Continue" }).click();
  }

  async updateAdminPassword(currentPassword: string, newPassword: string) {
    await this.inputCurrentPassword(currentPassword);
    await this.inputNewPassword(newPassword);
    await this.inputConfirmPassword(newPassword);
    await this.clickContinue();
  }
}
