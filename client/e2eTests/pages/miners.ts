import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

const PROLONGED_TIMEOUT = DEFAULT_TIMEOUT * 4;

export class MinersPage extends BasePage {
  async validateMinersPageOpened() {
    await this.validateTitle("Miners");
  }

  async validateAmountOfMiners(minerCount: number) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    await expect(rows).toHaveCount(minerCount);
  }

  async validateMinersAdded() {
    const rows = this.page.getByTestId("list-body").locator("tr");
    expect(await rows.count()).toBeGreaterThanOrEqual(5);
  }

  async filterMinersByType(minerType: string) {
    await this.click("Type");
    await this.page.locator(`//div[text()='${minerType}']/following-sibling::*//input`).click();
    await this.click("Apply");
  }

  async filterProtoMiners() {
    await this.filterMinersByType("Proto Rig");
    await this.waitForAntminersToDisappear();
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

  async clickMinerThreeDotsButton(ipAddress: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await minerRow.getByTestId(`single-miner-actions-menu-button`).click();
  }
  async clickMinerCheckbox(ipAddress: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await minerRow.locator(`//input[@type='checkbox']`).click();
  }

  async waitForMinersTitle() {
    await this.validateTitle("Miners");
  }

  async clickSelectAllCheckbox() {
    await this.page.getByTestId("list-header").locator('input[type="checkbox"]').click();
  }

  async clickActionsMenuButton() {
    await this.page.getByTestId("actions-menu-button").click();
  }

  async validateActionBarMinerCount(expectedCount: number) {
    await expect(this.page.getByTestId("action-bar")).toBeVisible();
    if (expectedCount === 1) {
      await expect(this.page.getByTestId("action-bar").getByText("1 miner selected")).toBeVisible();
    } else {
      await expect(this.page.getByTestId("action-bar").getByText(`${expectedCount} miners selected`)).toBeVisible();
    }
  }

  async clickWakeUpButton() {
    await this.page.getByTestId("wake-up-popover-button").click();
  }

  async clickWakeUpConfirm() {
    await this.page.getByTestId("wake-up-confirm-button").click();
  }

  async clickShutdownButton() {
    await this.page.getByTestId("shutdown-popover-button").click();
  }

  async clickShutdownConfirm() {
    await this.page.getByTestId("shutdown-confirm-button").click();
  }

  async clickUnpairButton() {
    await this.page.getByTestId("unpair-popover-button").click();
  }

  async clickUnpairConfirm() {
    await this.page.getByTestId("unpair-confirm-button").click();
  }

  async validateUpdateInProgress() {
    await expect(this.page.locator(`text=Update in progress`)).toBeVisible();
  }

  async validateUpdateCompleted() {
    await expect(this.page.locator(`text=Update in progress`)).toBeHidden();
  }

  async waitForMinersListToLoad() {
    const rows = this.page.getByTestId("list-body").locator("tr");
    await expect(rows).not.toHaveCount(0);
    await expect(async () => {
      const detectedRowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, 1000));
      expect(await rows.count()).toBe(detectedRowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async validateAllMinersStatus(expectedStatus: string) {
    await this.waitForMinersListToLoad();
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();
    for (let i = 0; i < rowCount; i++) {
      await rows.nth(i).scrollIntoViewIfNeeded();
      await expect(rows.nth(i).locator(`//td[@data-testid='status']`)).toContainText(expectedStatus, {
        timeout: PROLONGED_TIMEOUT,
      });
    }
  }

  async validateMinerStatus(ipAddress: string, expectedStatus: string) {
    await this.waitForMinersListToLoad();
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await expect(minerRow.locator(`//td[@data-testid='status']`)).toHaveText(expectedStatus, {
      timeout: PROLONGED_TIMEOUT,
    });
  }

  private async waitForColumnValuesToLoad(columnTestId: string) {
    await this.waitForMinersListToLoad();
    const rows = this.page.getByTestId("list-body").locator("tr");
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
    const rows = this.page.getByTestId("list-body").locator("tr");
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

  async getMinersCount(): Promise<number> {
    const rows = this.page.getByTestId("list-body").locator("tr");
    return await rows.count();
  }

  async getMinerIpAddressByIndex(index: number): Promise<string> {
    const rows = this.page.getByTestId("list-body").locator("tr");
    const row = rows.nth(index);
    return await row.getByTestId("ipAddress").innerText();
  }

  async validateMinerNotPresent(ipAddress: string) {
    const minerRow = this.page.getByTestId(`ipAddress`).getByText(ipAddress);
    await expect(minerRow).toBeHidden();
  }

  async clickAddMinersButton() {
    await this.click("Add miners");
  }

  async waitForAntminersToDisappear() {
    const antminerRows = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({ has: this.page.getByTestId("name").getByText("Antminer") });
    await expect(antminerRows).toHaveCount(0);
  }
}
