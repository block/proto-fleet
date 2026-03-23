import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { type IssueIconId } from "../helpers/testDataHelper";
import { BasePage } from "./base";

const PROLONGED_TIMEOUT = DEFAULT_TIMEOUT * 4;

export class MinersPage extends BasePage {
  private async clickDropdownFilterOption(popover: Locator, optionNames: string[]) {
    for (const optionName of optionNames) {
      const optionByTestId = popover.getByTestId(`filter-option-${optionName}`).first();
      if (await optionByTestId.isVisible().catch(() => false)) {
        await optionByTestId.click();
        return;
      }

      const optionByText = popover.getByText(optionName, { exact: true }).first();
      if (await optionByText.isVisible().catch(() => false)) {
        await optionByText.click();
        return;
      }
    }

    throw new Error(`Unable to find filter option. Tried: ${optionNames.join(", ")}`);
  }

  async validateMinersPageOpened() {
    await this.validateTitle("Miners");
  }

  async validateAmountOfMiners(minerCount: number) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    await expect(rows).toHaveCount(minerCount);
  }

  async validateMinersAdded(minerCount: number = 5) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    expect(await rows.count()).toBeGreaterThanOrEqual(minerCount);
  }

  private async filterMinersByModel(minerType: string) {
    await this.page.getByTestId("filter-dropdown-Model").click();
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await expect(popover).toHaveCSS("opacity", "1");
    await this.clickDropdownFilterOption(popover, [minerType]);
    await popover.getByRole("button", { name: "Apply" }).click();
    await expect(popover).toBeHidden();
  }

  async filterRigMiners() {
    await this.filterMinersByModel("Rig");
    await this.waitForAntminersToDisappear();
  }

  async waitForAntminersToDisappear() {
    const antminerRows = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({ has: this.page.getByTestId("name").getByText("Antminer") });
    await expect(antminerRows).toHaveCount(0);
  }

  async waitForRigMinersToDisappear() {
    const rigRows = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({ has: this.page.getByTestId("name").getByText("Rig") });
    await expect(rigRows).toHaveCount(0);
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

  async clickMinerCheckboxByIndex(index: number) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    const row = rows.nth(index);
    await row.scrollIntoViewIfNeeded();
    await row.locator('input[type="checkbox"]').first().click();
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

  async clickCoolingModeButton() {
    await this.page.getByTestId("cooling-mode-popover-button").click();
  }

  async validateAirCooledOptionSelected() {
    await expect(this.page.getByTestId("cooling-option-air").locator("input")).toBeChecked();
  }

  async clickAirCooledOption() {
    await this.page.getByTestId("cooling-option-air").locator("input").click();
  }

  async clickImmersionCooledOption() {
    await this.page.getByTestId("cooling-option-immersion").locator("input").click();
  }

  async clickUpdateCoolingModeConfirm() {
    await this.page.getByRole("button", { name: "Update cooling mode" }).click();
  }

  async clickRenameButton() {
    await this.page.getByTestId("rename-popover-button").click();
  }

  async validateBulkRenamePageOpened() {
    await this.validateTitle("Rename miners");
  }

  private bulkRenamePreviewContainer(): Locator {
    return this.isMobile
      ? this.page.getByTestId("bulk-rename-mobile-preview")
      : this.page.getByTestId("bulk-rename-desktop-preview");
  }

  async validateBulkRenamePreviewContainsName(name: string) {
    const container = this.bulkRenamePreviewContainer();
    await expect(container).toContainText(name);
  }

  async getBulkRenamePreviewName(): Promise<string> {
    const container = this.bulkRenamePreviewContainer();
    await expect(container).toBeVisible();

    const activeNewName = container.getByTestId("active-new-name").first();
    await expect(activeNewName).toBeVisible();
    return (await activeNewName.innerText()).trim();
  }

  async validateBulkRenamePreviewUnchangedPlaceholder() {
    const container = this.bulkRenamePreviewContainer();
    await expect(container).toBeVisible();
    await expect(container.getByTestId("active-new-name")).toHaveCount(0);
    await expect(container).toContainText("—");
  }

  async waitForBulkRenamePreviewName(expectedName: string) {
    await expect
      .poll(async () => await this.getBulkRenamePreviewName(), {
        timeout: DEFAULT_TIMEOUT,
      })
      .toBe(expectedName);
  }

  async clickBulkRenamePropertyToggle(propertyId: string) {
    await this.page.getByTestId(`bulk-rename-row-${propertyId}`).locator('label:has(input[type="checkbox"])').click();
  }

  async getBulkRenamePropertyOrder(): Promise<string[]> {
    const rows = this.page.locator('[data-testid^="bulk-rename-row-"]');
    const count = await rows.count();
    const propertyIds: string[] = [];

    for (let i = 0; i < count; i++) {
      const testId = await rows.nth(i).getAttribute("data-testid");
      if (testId) {
        propertyIds.push(testId.replace("bulk-rename-row-", ""));
      }
    }

    return propertyIds;
  }

  async ensureBulkRenamePropertyFirst(propertyId: string) {
    await expect(this.page.getByTestId(`bulk-rename-row-${propertyId}`)).toBeVisible();

    const order = await this.getBulkRenamePropertyOrder();
    if (order[0] === propertyId) {
      return;
    }

    const currentFirst = order[0];
    const source = this.page.getByTestId(`bulk-rename-reorder-${propertyId}`);
    const target = this.page.getByTestId(`bulk-rename-row-${currentFirst}`);

    await source.dragTo(target);

    await expect.poll(async () => (await this.getBulkRenamePropertyOrder())[0]).toBe(propertyId);
    // Wait for UI to stabilize after drag-and-drop to prevent race condition with subsequent property toggles
    await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
  }

  async toggleBulkRenameProperty(propertyId: string, enabled: boolean) {
    const row = this.page.getByTestId(`bulk-rename-row-${propertyId}`);
    const checkbox = row.locator('label:has(input[type="checkbox"]) input[type="checkbox"]');
    await expect(checkbox).toHaveCount(1);

    const isChecked = await checkbox.isChecked();
    if (isChecked !== enabled) {
      await this.clickBulkRenamePropertyToggle(propertyId);
      if (enabled) {
        await expect(checkbox).toBeChecked();
      } else {
        await expect(checkbox).not.toBeChecked();
      }
    }
  }

  async clickBulkRenamePropertyOptions(propertyId: string) {
    await this.page.getByTestId(`bulk-rename-options-${propertyId}`).click();
  }

  async dismissRenameOptionsModal() {
    const modal = this.page.getByTestId("modal");

    if (this.isMobile) {
      const cancelButton = modal.getByRole("button", { name: "Cancel", exact: true });
      await expect(cancelButton).toBeVisible();
      await cancelButton.click();
      await this.validateModalIsClosed();
      return;
    }

    const headerDismiss = modal.getByTestId("header-icon-button");
    const headerVisible = await headerDismiss.isVisible().catch(() => false);
    if (headerVisible) {
      await headerDismiss.click();
      await this.validateModalIsClosed();
      return;
    }

    const cancelButton = modal.getByRole("button", { name: "Cancel", exact: true });
    await expect(cancelButton).toBeVisible();
    await cancelButton.click();
    await this.validateModalIsClosed();
  }

  async fillCustomPropertyPrefix(prefix: string) {
    await this.page.getByTestId("custom-property-prefix-input").fill(prefix);
  }

  async fillCustomPropertySuffix(suffix: string) {
    await this.page.getByTestId("custom-property-suffix-input").fill(suffix);
  }

  async fillCustomPropertyCounterStart(value: string | number) {
    await this.page.getByTestId("custom-property-counter-start-input").fill(String(value));
  }

  async clickCustomPropertyCounterScale(counterScale: number) {
    const counterScaleGroup = this.page.getByRole("radiogroup", { name: "Counter scale" });
    await expect(counterScaleGroup).toBeVisible();

    const option = counterScaleGroup.getByTestId(`custom-property-counter-scale-option-${counterScale}`);
    await option.click();
    await expect(option.locator('input[type="radio"]')).toBeChecked();
  }

  async clickCustomPropertyTypeButton() {
    await this.page.getByTestId("custom-property-type-button").click();
  }

  async selectCustomPropertyType(typeId: string) {
    await this.clickCustomPropertyTypeButton();
    await this.page.getByTestId(`custom-property-type-option-${typeId}`).click();
  }

  async fillCustomPropertyStringValue(value: string) {
    await this.page.getByTestId("custom-property-string-input").fill(value);
  }

  async validateCustomPropertyPreviewText(expectedText: string) {
    await expect(
      this.page.getByTestId("custom-property-preview"),
      `Custom property preview should show "${expectedText}"`,
    ).toHaveText(expectedText);
  }

  async validateCustomPropertySaveDisabled() {
    const desktopSave = this.page.getByTestId("custom-property-options-save-button");
    const mobileSave = this.page.getByTestId("custom-property-options-save-button-mobile");

    const desktopVisible = await desktopSave.isVisible().catch(() => false);
    const mobileVisible = await mobileSave.isVisible().catch(() => false);

    expect(desktopVisible || mobileVisible, "Expected at least one Save button to be visible").toBe(true);

    if (desktopVisible) {
      await expect(desktopSave, "Desktop Save button should be disabled when counter start is empty").toBeDisabled();
    }

    if (mobileVisible) {
      await expect(mobileSave, "Mobile Save button should be disabled when counter start is empty").toBeDisabled();
    }
  }

  async clickFixedValueCharacterCountOption(option: number | "all") {
    const optionId = typeof option === "number" ? String(option) : option;
    const label = this.page.getByTestId(`fixed-value-character-count-option-${optionId}`);
    await label.click();
    await expect(label.locator('input[type="radio"]')).toBeChecked();
  }

  async clickFixedValueStringSectionOption(section: "first" | "last") {
    const label = this.page.getByTestId(`fixed-value-string-section-option-${section}`);
    await label.click();
    await expect(label.locator('input[type="radio"]')).toBeChecked();
  }

  async validateFixedValuePreviewText(expectedText: string) {
    await expect(
      this.page.getByTestId("fixed-value-preview"),
      `Fixed value preview should show "${expectedText}"`,
    ).toHaveText(expectedText);
  }

  async setCustomBulkRenameCounterScale(counterScale: number) {
    await this.clickBulkRenamePropertyOptions("custom");

    const counterStartInput = this.page.getByTestId("custom-property-counter-start-input");
    const isCounterStartVisible = await counterStartInput.isVisible();
    if (isCounterStartVisible) {
      const currentValue = (await counterStartInput.inputValue()).trim();
      if (currentValue === "") {
        await counterStartInput.fill("1");
      }
    }

    const counterScaleGroup = this.page.getByRole("radiogroup", { name: "Counter scale" });
    await expect(counterScaleGroup).toBeVisible();
    const option = counterScaleGroup.getByTestId(`custom-property-counter-scale-option-${counterScale}`);
    await option.click();
    await expect(option.locator('input[type="radio"]')).toBeChecked();

    await this.clickIn("Save", "modal");
    await this.validateModalIsClosed();
  }

  async clickBulkRenameSave() {
    await this.page.getByTestId("bulk-rename-save-button").click();
  }

  async selectBulkRenameSeparator(separatorId: string) {
    const separator = this.page.getByTestId(`bulk-rename-separator-${separatorId}`);
    const radio = separator.locator('input[type="radio"]');

    if (await radio.isChecked()) {
      return;
    }

    await separator.locator("xpath=ancestor::label").click();
    await expect(radio).toBeChecked();
  }

  async confirmBulkRenameWarningsIfPresent() {
    const duplicateNamesDialog = this.page.getByTestId("bulk-rename-duplicate-names-dialog");
    try {
      await duplicateNamesDialog.waitFor({ state: "visible", timeout: DEFAULT_INTERVAL });
      await duplicateNamesDialog.getByRole("button", { name: "Yes, continue" }).click();
    } catch {
      // Dialog not present, continue
    }

    const noChangesDialog = this.page.getByTestId("bulk-rename-no-changes-dialog");
    try {
      await noChangesDialog.waitFor({ state: "visible", timeout: DEFAULT_INTERVAL });
      await noChangesDialog.getByRole("button", { name: "Yes, continue" }).click();
    } catch {
      // Dialog not present, continue
    }
  }

  async fillRenameInput(name: string) {
    const input = this.page.getByTestId("rename-miner-input");
    await input.fill(name);
  }

  async clickRenameSave() {
    await this.clickIn("Save", "modal");
  }

  async validateMinerName(ipAddress: string, expectedName: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await expect(minerRow.getByTestId("name")).toContainText(expectedName);
  }

  async getMinerNameByIndex(index: number): Promise<string> {
    const rows = this.page.getByTestId("list-body").locator("tr");
    const row = rows.nth(index);
    await row.scrollIntoViewIfNeeded();
    return await row.getByTestId("name").innerText();
  }

  async getMinerNames(): Promise<string[]> {
    const nameElements = this.page.getByTestId("list-body").locator("tr").getByTestId("name");
    const names = await nameElements.allInnerTexts();
    return names.map((name) => name.trim());
  }

  async clickDeleteButton() {
    await this.page.getByTestId("delete-popover-button").click();
  }

  async clickDeleteConfirm() {
    await this.page.getByTestId("delete-confirm-button").click();
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
      const rowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
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
    const statusCell = minerRow.locator(`//td[@data-testid='status']`);
    const spinner = statusCell.locator('[class*="animate-spin"]');
    await expect(spinner).toBeHidden({
      timeout: PROLONGED_TIMEOUT,
    });
    await expect(statusCell).toHaveText(expectedStatus);
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
      expect(parts, `Expected temperature text to value and unit, but got: "${temperatureText}"`).toHaveLength(2);

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
    await this.clickButton("Add miners");
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
