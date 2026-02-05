import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class PoolModalPage extends BasePage {
  async validatePoolModalOpened() {
    await expect(this.page.getByTestId("modal")).toBeVisible();
  }

  async inputPoolName(name: string, poolIndex: number = 0) {
    await this.page.getByTestId(`pool-name-${poolIndex}-input`).fill(name);
  }

  async inputPoolUrl(url: string, poolIndex: number = 0) {
    await this.page.getByTestId(`url-${poolIndex}-input`).fill(url);
  }

  async inputPoolUsername(username: string, poolIndex: number = 0) {
    await this.page.getByTestId(`username-${poolIndex}-input`).fill(username);
  }

  async inputPoolPassword(password: string, poolIndex: number = 0) {
    await this.page.getByTestId(`password-${poolIndex}-input`).fill(password);
  }

  async clickTestConnection() {
    await this.page.locator(`//button//*[text()='Test connection']`).click();
  }

  async validateConnectionSuccessful() {
    await expect(
      this.page.locator(`//div[@data-testid='pool-connected-callout' and not(contains(@class,'hidden'))]`),
    ).toBeVisible();
  }

  async clickSave() {
    await this.click("Save");
  }

  async clickAddPool() {
    await this.click("Add pool");
  }
}
