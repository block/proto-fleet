import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPoolsPage extends BasePage {
  async validateMiningPoolsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/mining-pools/);
  }

  async clickAddPool() {
    await this.click("Add pool");
  }

  async validatePoolModalOpened() {
    await expect(this.page.getByTestId("modal").getByText(`Default mining pool`).first()).toBeVisible();
  }

  async inputPoolName(name: string) {
    await this.page.getByTestId(`name-0-input`).fill(name);
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
    await expect(this.page.getByText(`We couldn't connect with your pool.`)).toBeVisible();
  }

  async validateConnectionSuccessful() {
    await expect(this.page.getByText(`Pool connection successful`)).toBeVisible();
  }

  async clickSavePool() {
    await this.page.getByTestId("pool-save-button").click();
  }

  async validatePoolEntryByUniqueName(expectedName: string, expectedUrl: string, expectedUsername: string) {
    await expect(this.page.getByTestId(`pool-row`).getByTestId("pool-name").getByText(expectedName)).toBeVisible();
    const row = this.page
      .getByTestId(`pool-row`)
      .filter({ has: this.page.getByTestId("pool-name").getByText(expectedName) });
    await expect(row.getByTestId("pool-url").getByText(expectedUrl)).toBeVisible();
    await expect(row.getByTestId("pool-username").getByText(expectedUsername)).toBeVisible();
  }
}
