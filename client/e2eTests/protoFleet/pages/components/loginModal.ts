import { testConfig } from "../../config/test.config";
import { BasePage } from "../base";

export class LoginModalComponent extends BasePage {
  async loginAsAdmin() {
    const headingText = "Log in to update your pool settings";
    await this.validateTitleInModal(headingText);
    const modal = this.page.getByTestId("modal");

    await modal.locator("xpath=.//input[@id='username']").fill(testConfig.users.admin.username);
    await modal.locator("xpath=.//input[@id='password']").fill(testConfig.users.admin.password);
    await modal.getByRole("button", { name: "Continue" }).click();

    await this.validateTitleInModalNotVisible(headingText);
  }
}
