import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class AuthPage extends BasePage {
  async isAlreadyLoggedIn(timeoutMs = 5000): Promise<boolean> {
    // Probe for the post-login marker (mobile menu button or desktop logout
    // button) to decide whether the full login flow can be skipped.
    try {
      await this.validateLoggedIn(timeoutMs);
      return true;
    } catch {
      return false;
    }
  }

  async inputUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  async inputPassword(password: string) {
    await this.page.locator(`//input[@id='password']`).fill(password);
  }

  async clickLogin() {
    await this.page.locator(`//button[@data-testid="login-button"]`).click();
  }

  async validateRedirectedToAuth() {
    await expect(this.page).toHaveURL(/.*\/auth/);
  }

  async inputNewPassword(password: string) {
    await this.page.locator(`//input[@id='newPassword']`).fill(password);
  }

  async inputConfirmPassword(password: string) {
    await this.page.locator(`//input[@id='confirmPassword']`).fill(password);
  }

  async clickContinue() {
    await this.clickButton("Continue");
  }

  async clickLoginButton() {
    await this.clickButton("Login");
  }

  async clickPasswordVisibilityToggle() {
    await this.page.locator(`//*[@data-testid="eye-icon"]`).click();
  }

  async validateInvalidCredentials() {
    await expect(this.page.getByText("Invalid credentials entered.")).toBeVisible();
  }

  async validateUpdatePasswordTitle() {
    await this.validateTitle("Update Your Password");
  }

  async validatePasswordSaved() {
    await this.validateTitle("Password saved");
  }

  async clickCreateAccount() {
    await this.clickButton("Create an account");
  }

  async validateCreateCredentialsPrompt() {
    await expect(this.page.getByText("Create your username and password")).toBeVisible();
  }

  async clickGetStarted() {
    await this.clickButton("Get started");
  }
}
