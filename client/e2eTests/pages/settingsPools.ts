import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPoolsPage extends BasePage {
  async validateMiningPoolsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/mining-pools/);
  }

  async validateMiningPoolsDescription() {
    await this.validateTitle("Pools");
  }

  async getDefaultPoolIndex(): Promise<number> {
    return 0;
  }

  async clickAddPool(poolIndex: number) {
    await this.page.locator(`//*[@data-testid="pool-${poolIndex}-add-button"]`).click();
  }

  async validatePoolModalOpened() {
    await expect(this.page.locator(`//*[@data-testid="modal"]`).getByText(`Default mining pool`).first()).toBeVisible();
  }

  async inputPoolUrl(poolIndex: number, url: string) {
    await this.page.locator(`//input[@id='url ${poolIndex}']`).fill(url);
  }

  async inputPoolUsername(poolIndex: number, username: string) {
    await this.page.locator(`//input[@id='username ${poolIndex}']`).fill(username);
  }

  async clickTestConnection() {
    await this.page.locator(`//button//*[text()='Test connection']`).click();
  }

  async validateConnectionFailed() {
    await expect(this.page.getByText(`We couldn't connect with your pool.`)).toBeVisible();
  }

  async clickDismissModal() {
    // TODO: Work around the fact that in popup there are 3 'dismiss' buttons, others invisible
    // await this.page.locator(`//*[@data-testid="modal"]`).getByRole("button", { name: "Dismiss" }).click();
  }

  async clickSavePool() {
    await this.page.locator(`//*[@data-testid="pool-save-button"]`).click();
  }

  async validatePoolUrlSaved(poolIndex: number, expectedUrl: string) {
    await expect(this.page.locator(`//*[@data-testid="pool-${poolIndex}-saved-url"]`)).toHaveText(expectedUrl);
  }

  async validatePoolNotConfigured() {
    await expect(
      this.page.locator(`//*[text()='Not configured'][preceding-sibling::*[text()='Default pool']]`),
    ).toBeVisible();
  }
}
