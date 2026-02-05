import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class NewPoolModalPage extends BasePage {
  async validatePoolModalOpened() {
    await expect(this.page.getByTestId("modal").getByText(`Default mining pool`).first()).toBeVisible();
  }

  async inputPoolName(name: string) {
    await this.page.getByTestId(`pool-name-0-input`).fill(name);
  }

  async inputPoolUrl(url: string) {
    await this.page.getByTestId(`url-0-input`).fill(url);
  }

  async inputPoolUsername(username: string) {
    await this.page.getByTestId(`username-0-input`).fill(username);
  }

  async clickTestConnection() {
    await this.page.locator(`//button//*[text()='Test connection']`).click();
  }

  async validateConnectionFailed() {
    await expect(
      this.page.locator(`//div[@data-testid='pool-not-connected-callout' and not(contains(@class,'hidden'))]`),
    ).toBeVisible();
  }

  async validateEmptyPoolUrlError() {
    await this.validateTextIsVisible("A Pool URL is required to connect to this pool.");
  }

  async validateConnectionSuccessful() {
    await expect(
      this.page.locator(`//div[@data-testid='pool-connected-callout' and not(contains(@class,'hidden'))]`),
    ).toBeVisible();
  }

  async clickSaveNewPool() {
    await this.page.getByTestId("pool-save-button").click();
  }
}
