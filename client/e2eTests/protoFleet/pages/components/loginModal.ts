import { testConfig } from "../../config/test.config";
import { submitWithPasswordAttemptRetry } from "../../helpers/passwordAttemptRetry";
import { BasePage } from "../base";

export class LoginModalComponent extends BasePage {
  private async loginAsAdminForTitle(headingText: string) {
    await this.validateTitleInModal(headingText);
    const modal = this.page.getByTestId("modal");

    await modal.locator("xpath=.//input[@id='username']").fill(testConfig.users.admin.username);
    await modal.locator("xpath=.//input[@id='password']").fill(testConfig.users.admin.password);
    const response = await submitWithPasswordAttemptRetry(this.page, /\/VerifyCredentials$/, async () => {
      await modal.getByRole("button", { name: "Continue" }).click();
    });
    if (!response.ok()) {
      throw new Error(`Credential verification failed: HTTP ${response.status()}`);
    }

    await this.validateTitleInModalNotVisible(headingText);
  }

  async loginAsAdmin() {
    await this.loginAsAdminForTitle("Log in to update your pool settings");
  }

  async loginAsAdminForWorkerNames() {
    await this.loginAsAdminForTitle("Log in to update worker names");
  }

  async loginAsAdminForSecurity() {
    await this.loginAsAdminForTitle("Log in to update your security settings");
  }
}
