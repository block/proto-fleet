import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class PoolsPage extends BasePage {
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
    await this.clickButton("Save");
  }

  async clickAddPool() {
    await this.clickButton("Add pool");
  }

  async clickAddAnotherPool() {
    await this.clickButton("Add another pool");
  }

  async validateUrlValidationError(poolIndex: number, message: string) {
    await expect(this.page.getByTestId(`url-${poolIndex}-input-validation-error`)).toBeVisible();
    await expect(this.page.getByTestId(`url-${poolIndex}-input-validation-error`)).toHaveText(message);
  }

  async validateConnectionFailed() {
    await expect(this.page.getByTestId("pool-not-connected-callout")).toBeVisible();
    await this.validateTextInModal("We couldn't connect with your pool. Review your pool details and try again.");
  }

  async closePoolNotConnectedCallout() {
    await this.page.getByTestId("pool-not-connected-callout").getByRole("button").click();
  }

  async validateSaveButtonDisabled() {
    await expect(this.page.getByTestId("modal").getByRole("button", { name: "Save" })).toBeDisabled();
  }

  async validateSaveButtonEnabled() {
    await expect(this.page.getByTestId("modal").getByRole("button", { name: "Save" })).toBeEnabled();
  }

  async validateCalloutWithText(text: string) {
    await expect(this.page.getByTestId("callout")).toBeVisible();
    await expect(this.page.getByTestId("callout").getByText(text)).toBeVisible();
  }

  async closeCallout() {
    await this.page.getByTestId("callout").getByRole("button").click();
  }

  async clickMiningPoolButton() {
    await this.clickButton("Mining Pool");
  }

  async validatePoolInfoPopoverVisible() {
    await expect(this.page.getByTestId("pool-info-popover")).toBeVisible();
  }

  async validateTitleInPopover(title: string) {
    await expect(
      this.page.getByTestId("pool-info-popover").locator(`//*[contains(@class,'heading')][text()="${title}"]`),
    ).toBeVisible();
  }

  async validateTextInPopover(text: string) {
    await expect(this.page.getByTestId("pool-info-popover").getByText(text)).toBeVisible();
  }

  async validateExactTextInPopover(text: string) {
    await expect(this.page.getByTestId("pool-info-popover").getByText(text, { exact: true })).toBeVisible();
  }

  async clickViewMiningPools() {
    await this.page.getByTestId("pool-info-popover").getByRole("button", { name: "View mining pools" }).click();
  }

  async validatePoolRowCount(expectedCount: number) {
    const poolRows = this.page.getByTestId("pool-row");
    await expect(poolRows).toHaveCount(expectedCount);
  }

  async validatePoolRowDetails(poolIndex: number, poolName: string, poolUrl: string) {
    const poolRows = this.page.getByTestId("pool-row");
    const targetRow = poolRows.nth(poolIndex);

    await expect(targetRow).toBeVisible();
    await expect(targetRow.getByText(poolName)).toBeVisible();
    await expect(targetRow.getByTestId(`pool-${poolIndex}-saved-url`)).toHaveText(poolUrl);
  }
}
