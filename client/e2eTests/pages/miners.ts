import { expect, type Locator } from "@playwright/test";
import { BasePage } from "./base";

export class MinersPage extends BasePage {
  async validateMinersPageOpened() {
    await this.validateTitle("Miners");
  }

  async validateAmountOfMiners(minerCount: number) {
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    await expect(rows).toHaveCount(minerCount);
  }

  async getMinerRowByIp(ipAddress: string): Promise<Locator> {
    return this.page.locator(`//tr[child::*[@data-testid="ipAddress" and descendant::text()='${ipAddress}']]`);
  }

  async validateMinerInList(ipAddress: string) {
    await expect(await this.getMinerRowByIp(ipAddress)).toBeVisible();
  }

  async validateMinerValue(minerName: string, columnTestId: string, expectedValue: string) {
    const minerRow = await this.getMinerRowByIp(minerName);
    const columnLocator = minerRow.locator(`//td[@data-testid='${columnTestId}']`);
    await expect(columnLocator).toHaveText(expectedValue);
  }

  async clickMinerCheckbox(ipAddress: string) {
    (await this.getMinerRowByIp(ipAddress)).locator(`//input[@type='checkbox']`).check();
  }

  async waitForMinersTitle() {
    await this.validateTitle("Miners");
  }

  async clickSelectAllCheckbox() {
    await this.page.locator(`//*[@data-testid="list-header"]//input[@type="checkbox"]`).click();
  }

  async clickActionsMenuButton() {
    await this.page.locator(`//*[@data-testid="actions-menu-button"]`).click();
  }

  async clickWakeUpButton() {
    await this.page.locator(`//*[@data-testid="wake-up-popover-button"]`).click();
  }

  async clickWakeUpConfirm() {
    await this.page.locator(`//*[@data-testid="wake-up-confirm-button"]`).click();
  }

  async clickShutdownButton() {
    await this.page.locator(`//*[@data-testid="shutdown-popover-button"]`).click();
  }

  async clickShutdownConfirm() {
    await this.page.locator(`//*[@data-testid="shutdown-confirm-button"]`).click();
  }

  async validateUpdateInProgress() {
    await expect(this.page.locator(`text=Update in progress`)).toBeVisible({ timeout: 2000 });
  }

  async validateUpdateCompleted() {
    await expect(this.page.locator(`text=Update in progress`)).toBeHidden({ timeout: 15000 });
  }

  async waitForMinersListToLoad() {
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    await expect(rows).not.toHaveCount(0);

    const initialCount = await rows.count();
    await expect(async () => {
      const currentCount = await rows.count();
      expect(currentCount).toBe(initialCount);
    }).toPass({ timeout: 5000, intervals: [500] });
  }

  async validateAllMinersStatus(expectedStatus: string) {
    await this.waitForMinersListToLoad();
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    const rowCount = await rows.count();
    for (let i = 0; i < rowCount; i++) {
      await rows.nth(i).scrollIntoViewIfNeeded();
      await expect(rows.nth(i).locator(`//td[@data-testid='status']`)).toContainText(expectedStatus);
    }
  }
}
