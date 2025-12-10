import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class MinersPage extends BasePage {
  async validateMinersPageOpened() {
    await this.validateTitle("Miners");
  }

  async validateAmountOfMiners(minerCount: number) {
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    await expect(rows).toHaveCount(minerCount);
  }

  async filterMinersByType(minerType: string) {
    await this.click("Type");
    await this.page.locator(`//div[text()='${minerType}']/following-sibling::*//input`).click();
    await this.click("Apply");
  }

  async filterProtoMiners() {
    this.filterMinersByType("Proto Rig");
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
    await expect(this.page.locator(`text=Update in progress`)).toBeVisible();
  }

  async validateUpdateCompleted() {
    await expect(this.page.locator(`text=Update in progress`)).toBeHidden();
  }

  async waitForMinersListToLoad() {
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    await expect(rows).not.toHaveCount(0);
    const initialCount = await rows.count();
    await expect(async () => {
      const currentCount = await rows.count();
      expect(currentCount).toBe(initialCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
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

  private async waitForColumnValuesToLoad(columnTestId: string) {
    await this.waitForMinersListToLoad();
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    const rowCount = await rows.count();
    for (let i = 0; i < rowCount; i++) {
      await rows.nth(i).scrollIntoViewIfNeeded();
      await expect(async () => {
        const locator = rows.nth(i).locator(`//td[@data-testid='${columnTestId}']`);
        await expect(locator).not.toHaveText("", { timeout: 5000 });
        await expect(locator).not.toHaveText("N/A", { timeout: 5000 });
      }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
    }
  }

  async waitForTemperaturesToLoad() {
    await this.waitForColumnValuesToLoad("temperature");
  }

  private async validateTemperatureUnit(expectedUnit: string) {
    await this.waitForTemperaturesToLoad();
    const rows = this.page.locator(`//*[@data-testid='list-body']/tr`);
    const rowCount = await rows.count();
    for (let i = 0; i < rowCount; i++) {
      await rows.nth(i).scrollIntoViewIfNeeded();

      // Get temperature text
      const temperatureText = await rows.nth(i).locator(`//td[@data-testid='temperature']`).innerText();
      const parts = temperatureText.split(" ");
      expect(parts.length).toBe(2);

      // Validate unit - C/F
      const unit = parts[1];
      expect(unit).toBe(expectedUnit);

      // Validate temperature value
      const value = parseFloat(parts[0]);
      if (expectedUnit === "°F") {
        expect(value).toBeGreaterThanOrEqual(70.0);
      } else {
        expect(value).toBeGreaterThanOrEqual(0);
        expect(value).toBeLessThanOrEqual(100.0);
      }
    }
  }

  async validateTemperatureUnitFahrenheit() {
    await this.validateTemperatureUnit("°F");
  }

  async validateTemperatureUnitCelsius() {
    await this.validateTemperatureUnit("°C");
  }

  async validateActiveFilter(filterLabel: string) {
    const activeFilterButton = this.page.locator(`[data-testid*="active-filter-"]`, { hasText: filterLabel });
    await expect(activeFilterButton).toBeVisible();
  }
}
