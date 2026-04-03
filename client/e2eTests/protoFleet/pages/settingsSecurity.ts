import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsSecurityPage extends BasePage {
  async clickUpdateUsername() {
    await this.clickIn("Update", "username-row");
  }

  async clickUpdatePassword() {
    await this.clickIn("Update", "password-row");
  }

  async inputCurrentPassword(password: string) {
    await this.page.locator(`//input[@id='currentPassword']`).fill(password);
  }

  async clickConfirm() {
    await this.clickIn("Confirm", "modal");
  }

  async inputNewUsername(username: string) {
    await this.page.locator(`//input[@id='newUsername']`).fill(username);
  }

  async clickConfirmUsername() {
    await this.clickIn("Confirm", "modal");
  }

  async validateUsernameChangeToast() {
    await expect(this.page.getByText(`Username updated`)).toBeVisible();
  }

  async validateUsername(username: string) {
    await expect(this.page.getByTestId("username-value")).toHaveText(username);
  }

  async inputNewPassword(password: string) {
    await this.page.locator(`//input[@id='newPassword']`).fill(password);
  }

  async inputConfirmPassword(password: string) {
    await this.page.locator(`//input[@id='confirmPassword']`).fill(password);
  }

  async clickConfirmPassword() {
    await this.clickIn("Confirm", "modal");
  }

  async validatePasswordChangeToast() {
    await expect(this.page.getByText(`Password updated`)).toBeVisible();
  }
}
