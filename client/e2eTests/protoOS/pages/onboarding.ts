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

  async handleNoFansDetectedDialogIfVisible() {
    const noFansTitle = this.page.getByText("No fans detected");

    const dialogAppeared = await noFansTitle
      .waitFor({ state: "visible", timeout: 5000 })
      .then(() => true)
      .catch(() => false);

    if (!dialogAppeared) return;

    await this.page.getByRole("button", { name: "Use air cooling" }).click();
    await expect(noFansTitle).toBeHidden();
  }

  async waitForMinerReadyScreen() {
    const loadingTitle = this.page.locator(`//*[contains(@class,'heading')][text()="Configuring your miner"]`);
    const readyTitle = this.page.locator(`//*[contains(@class,'heading')][text()="Your miner is ready"]`);

    await Promise.race([
      readyTitle.waitFor({ state: "visible" }),
      loadingTitle.waitFor({ state: "visible" }).then(async () => {
        await readyTitle.waitFor({ state: "visible" });
      }),
    ]);
  }
}
