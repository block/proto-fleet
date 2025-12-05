import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class HomePage extends BasePage {
  async validateCompleteSetupTitle() {
    await this.validateTitle("Complete setup");
  }

  async clickAuthenticateMinersButton() {
    await this.click("Authenticate");
  }

  async validateAuthenticateMinersModalTitle() {
    await this.validateTitleInModal("Authenticate miners");
  }

  async inputMinerAuthUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  async inputMinerAuthPassword(password: string) {
    await this.page.locator(`//input[@id='password']`).fill(password);
  }

  async clickAuthenticateMinersConfirmButton() {
    await this.page.locator(`//*[@data-testid='modal']`).getByRole("button", { name: "Authenticate" }).click();
  }

  async validateCompleteSetupTitleNotVisible() {
    await this.validateTitleNotVisible("Complete setup");
  }

  async validateAuthenticateMinersButtonNotVisible() {
    await expect(this.page.getByRole("button", { name: "Authenticate" })).toBeHidden();
  }
}
