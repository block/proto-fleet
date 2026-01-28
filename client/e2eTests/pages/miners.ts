import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { type IssueIconId } from "../helpers/testDataHelper";
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

  private async filterMinersByType(minerType: string) {
    await this.click("Type");
    // Filter glitches if done too quickly
    await this.waitForColumnValuesToLoad("hashrate");
    await this.page.locator(`//div[text()='${minerType}']/following-sibling::*//input`).click();
    await this.click("Apply");
  }

  async filterProtoMiners() {
    await this.filterMinersByType("Proto Rig");
    await this.waitForBitmainMinersToDisappear();
  }

  async filterBitmainMiners() {
    await this.filterMinersByType("Bitmain");
    await this.waitForProtoMinersToDisappear();
  }

  async waitForBitmainMinersToDisappear() {
    const bitmainRows = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({ has: this.page.getByTestId("name").getByText("Bitmain") });
    await expect(bitmainRows).toHaveCount(0);
  }

  async waitForProtoMinersToDisappear() {
    const protoRigRows = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({ has: this.page.getByTestId("name").getByText("Proto") });
    await expect(protoRigRows).toHaveCount(0);
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

  async validateMinerIcon(minerIp: string, columnTestId: string, iconId: IssueIconId) {
    const minerRow = await this.getMinerRowByIp(minerIp);
    const columnLocator = minerRow.locator(`//td[@data-testid='${columnTestId}']`);
    await expect(columnLocator.getByTestId(iconId)).toBeVisible();
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

  async uncheckSelectAllCheckbox() {
    const checkbox = this.page.getByTestId("list-header").locator('input[type="checkbox"]');
    if (await checkbox.isChecked()) {
      await checkbox.click();
    }
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

  async clickRebootButton() {
    await this.page.getByTestId("reboot-popover-button").click();
  }

  async clickRebootConfirm() {
    await this.page.getByTestId("reboot-confirm-button").click();
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

  async clickManagePowerButton() {
    await this.page.getByTestId("manage-power-popover-button").click();
  }

  async clickMaxPowerOption() {
    await this.page.getByTestId("power-option-maximize").locator("input").click();
  }

  async clickReducePowerOption() {
    await this.page.getByTestId("power-option-reduce").locator("input").click();
  }

  async clickManagePowerConfirm() {
    await this.clickIn("Confirm", "modal");
  }

  async clickEditMiningPoolButton() {
    await this.page.getByTestId("mining-pool-popover-button").click();
  }

  async clickUnpairButton() {
    await this.page.getByTestId("unpair-popover-button").click();
  }

  async clickUnpairConfirm() {
    await this.page.getByTestId("unpair-confirm-button").click();
  }

  async validateUpdateInProgress() {
    await expect(this.page.getByText(/Update in progress|updates in progress/)).toBeVisible();
  }

  async validateUpdateCompleted() {
    await expect(this.page.getByText(/Update in progress|updates in progress/)).toBeHidden();
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

  async validateAllMinersStatus(status: string, expected: boolean = true) {
    await this.waitForColumnValuesToLoad("status");
    // To avoid miner actions hiding some valuable data in screenshots
    await this.uncheckSelectAllCheckbox();
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();
    // Start from last row to avoid extremely long tests due to lazy loading
    for (let i = rowCount - 1; i >= 0; i--) {
      await rows.nth(i).scrollIntoViewIfNeeded();
      const statusLocator = rows.nth(i).locator(`//td[@data-testid='status']`);
      if (expected) {
        await expect(statusLocator).toContainText(status, {
          timeout: PROLONGED_TIMEOUT,
        });
      } else {
        await expect(statusLocator).not.toContainText(status, {
          timeout: PROLONGED_TIMEOUT,
        });
      }
    }
  }

  async validateNoMinerWithStatus(status: string) {
    await this.validateAllMinersStatus(status, false);
  }

  async getMinerStatus(ipAddress: string): Promise<string> {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    return await minerRow.locator(`//td[@data-testid='status']`).innerText();
  }

  async validateMinerStatus(ipAddress: string, expectedStatus: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await expect(minerRow.locator(`//td[@data-testid='status']`)).toHaveText(expectedStatus, {
      timeout: PROLONGED_TIMEOUT,
    });
  }

  private async waitForColumnValuesToLoad(columnTestId: string) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();
    // Start from last row to avoid extremely long tests due to lazy loading
    for (let i = rowCount - 1; i >= 0; i--) {
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
      const temperatureLocator = rows.nth(i).locator(`//td[@data-testid='temperature']`);
      await temperatureLocator.scrollIntoViewIfNeeded();

      // Get temperature text
      const temperatureText = await temperatureLocator.innerText();
      const parts = temperatureText.split(" ");
      expect(parts.length, `Expected temperature text to value and unit, but got: "${temperatureText}"`).toBe(2);

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
    const minerRow = this.page.getByTestId(`ipAddress`).getByText(ipAddress, { exact: true });
    await expect(minerRow).toBeHidden();
  }

  async clickAddMinersButton() {
    await this.click("Add miners");
  }

  async clickMinerElementByTestId(ipAddress: string, testId: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await minerRow.getByTestId(testId).click();
  }

  async validateMinerIssuesModalOpened(minerName: string) {
    await this.validateTitleInModal(`${minerName} status`);
  }

  async validateErrorInModal(errorText: string, iconId: IssueIconId) {
    const modal = this.page.locator('[role="dialog"], [data-testid*="modal"]');
    await expect(modal.getByText(errorText)).toBeVisible();
    await expect(modal.getByTestId(iconId)).toBeVisible();
    await expect(modal.getByText("Reported on 01/01/2026 at ").first()).toBeVisible();
  }

  async clickCloseStatusModal() {
    await this.clickIn("Done", "modal");
  }
}
