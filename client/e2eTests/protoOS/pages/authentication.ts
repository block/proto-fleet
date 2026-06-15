import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class AuthenticationPage extends BasePage {
  async validateOnboardingAuthenticationUrl() {
    await expect(this.page).toHaveURL(/.*\/onboarding\/authentication/);
  }

  async validateSettingsAuthenticationUrl() {
    await expect(this.page).toHaveURL(/.*\/settings\/authentication/);
  }

  async validateLoginRequiredModal() {
    await this.validateModalIsOpen();
    await this.validateTitleInModal("Login required");
    await this.validateButtonIsVisible("Cancel");
    await expect(this.page.getByTestId("login-button")).toBeVisible();
  }

  async dismissLoginRequiredModal() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Cancel" }).click();
  }
}
