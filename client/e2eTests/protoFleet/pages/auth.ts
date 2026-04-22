import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class AuthPage extends BasePage {
  async isAlreadyLoggedIn(timeoutMs = 5000): Promise<boolean> {
    // Wait for the page to settle into either the authenticated UI (mobile
    // menu button or desktop logout button) or the login form. Return true
    // only after positively confirming the authenticated state. Any other
    // locator failure (e.g. selector rename, browser crash) is re-thrown so
    // auth regressions surface here rather than as a secondary failure
    // inside the login flow.
    const loggedInMarker = this.isMobile
      ? this.page.getByTestId("navigation-menu-button")
      : this.page.getByTestId("logout-button");
    const loginForm = this.page.locator(`//input[@id='username']`);

    try {
      await expect(loggedInMarker.or(loginForm)).toBeVisible({ timeout: timeoutMs });
    } catch (err) {
      // Only swallow Playwright timeouts (page hasn't settled into either
      // state) so the caller can fall through to the full login flow which
      // has its own assertion.
      if (err instanceof Error && /Timeout/i.test(err.message)) {
        return false;
      }
      throw err;
    }

    return await loggedInMarker.isVisible();
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
