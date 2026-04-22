import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class WelcomePage extends BasePage {
  async validateWelcomeUrl() {
    await expect(this.page).toHaveURL(/.*\/onboarding\/welcome/);
  }

  async validateAuthenticationUrl() {
    await expect(this.page).toHaveURL(/.*\/onboarding\/authentication/);
  }

  async validateVerifyUrl() {
    await expect(this.page).toHaveURL(/.*\/onboarding\/verify/);
  }

  async inputPassword(password: string) {
    await this.page.locator('input[id="password"]').fill(password);
  }

  async inputConfirmPassword(password: string) {
    await this.page.locator('input[id="confirmPassword"]').fill(password);
  }

  async clickContinue() {
    await this.clickButton("Continue");
  }

  async validateUsernameFieldDisabledWithValue(expectedValue: string) {
    const usernameField = this.page.locator('input[id="username"]');
    await expect(usernameField).toBeDisabled();
    await expect(usernameField).toHaveValue(expectedValue);
  }

  async clickGetStartedButton() {
    await this.clickButton("Get Started");
  }

  async clickContinueSetup() {
    await this.clickButton("Continue setup");
  }

  async validateMiningPoolUrl() {
    await expect(this.page).toHaveURL(/.*\/onboarding\/mining-pool/);
  }

  async validateDefaultPoolWarningVisible() {
    await expect(this.page.getByTestId("warn-default-pool-callout")).toBeVisible();
  }

  async validateDefaultPoolWarningText() {
    await this.validateTextIsVisible("A default pool is required to set up your miner.");
  }

  async closeDefaultPoolWarning() {
    await this.page.getByTestId("warn-default-pool-callout").getByRole("button").click();
  }
}
