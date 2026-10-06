import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class NewPoolModalPage extends BasePage {
  private modal() {
    return this.page.getByTestId("modal");
  }

  async validatePoolModalOpened() {
    await expect(this.modal().getByText(`Default mining pool`).first()).toBeVisible();
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
    const modal = this.modal();
    const desktopButton = modal.getByTestId("pool-test-connection-button");

    if (await desktopButton.isVisible().catch(() => false)) {
      await desktopButton.click();
      return;
    }

    await modal.getByTestId("overflow-menu-trigger").click();
    await this.page
      .getByTestId("modal-overflow-sheet-content")
      .getByTestId("pool-test-connection-button-overflow-item")
      .click();
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
    const modal = this.modal();
    const saveButton = modal.getByTestId(this.isMobile ? "pool-save-button-mobile" : "pool-save-button");

    if (await saveButton.isVisible().catch(() => false)) {
      await saveButton.click();
      return;
    }

    await modal.getByTestId("pool-save-button").click();
  }
}
