import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class AuthenticationPage extends BasePage {
  async validateUsernameFieldDisabledWithValue(expectedValue: string) {
    const usernameField = this.page.locator('input[id="username"]:not([data-testid="username"])');
    await expect(usernameField).toBeDisabled();
    await expect(usernameField).toHaveValue(expectedValue);
  }

  async inputCurrentPassword(password: string) {
    await this.page.locator('input[id="currentPassword"]').fill(password);
  }

  async inputNewPassword(password: string) {
    await this.page.locator('input[id="password"]:not([data-testid="password"])').fill(password);
  }

  async inputConfirmPassword(password: string) {
    await this.page.locator('input[id="confirmPassword"]').fill(password);
  }

  async clickContinue() {
    await this.page.locator('button:not([data-testid="login-button"])', { hasText: "Continue" }).click();
  }

  async updateAdminPassword(currentPassword: string, newPassword: string) {
    await this.inputCurrentPassword(currentPassword);
    await this.inputNewPassword(newPassword);
    await this.inputConfirmPassword(newPassword);
    await this.clickContinue();
  }
}
