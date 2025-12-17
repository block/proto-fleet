import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class HomePage extends BasePage {
  async validateCompleteSetupTitle() {
    await this.validateTitle("Complete setup");
  }

  async validateHomePageOpened() {
    await expect(this.page).toHaveURL(/.*\/$/);
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
    await this.page.getByTestId("modal").getByRole("button", { name: "Authenticate" }).click();
  }

  async validateCompleteSetupTitleNotVisible() {
    await this.validateTitleNotVisible("Complete setup");
  }

  async validateAuthenticateMinersButtonNotVisible() {
    await expect(this.page.getByRole("button", { name: "Authenticate" })).toBeHidden();
  }

  async clickControlBoardsLink() {
    await this.page.getByRole("link", { name: "Control Boards" }).click();
  }

  async clickFansLink() {
    await this.page.getByRole("link", { name: "Fans" }).click();
  }

  async clickHashboardsLink() {
    await this.page.getByRole("link", { name: "Hashboards" }).click();
  }

  async clickPowerSuppliesLink() {
    await this.page.getByRole("link", { name: "Power supplies" }).click();
  }
}
