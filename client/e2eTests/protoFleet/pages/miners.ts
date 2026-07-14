import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { PROTO_RIG_DISPLAY_NAME, PROTO_RIG_MODEL } from "../helpers/minerModels";
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
    await expect(this.page).toHaveURL(/.*\/fleet\/miners/);
    await this.validateTitle("Fleet");
  }

  async validateAmountOfMiners(minerCount: number) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    await expect(rows).toHaveCount(minerCount);
  }

  async validateMinersAdded(minerCount: number = 5) {
    const rows = this.page.getByTestId("list-body").locator("tr");
    await expect
      .poll(() => rows.count(), { timeout: PROLONGED_TIMEOUT, intervals: [DEFAULT_INTERVAL] })
      .toBeGreaterThanOrEqual(minerCount);
  }

  private async openAddFilterPopover() {
    await this.page.getByTestId("filter-nested-filters-meta").click();
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");
    await expect(popover).toBeVisible();
    return popover;
  }

  private async openModelSubmenu(popover: Locator) {
    await popover.getByTestId("nested-dropdown-filter-row-model").click();
    // Desktop renders a portaled side submenu; phone/tablet collapses options into the
    // parent popover with a "back" header. Either way the option rows for the chosen
    // category become visible — return whichever container holds them.
    const desktopSubmenu = this.page.getByTestId("nested-dropdown-filter-submenu-model");
    const mobileBack = popover.getByTestId("nested-dropdown-filter-back");
    await expect(desktopSubmenu.or(mobileBack)).toBeVisible();
    if (await desktopSubmenu.isVisible().catch(() => false)) return desktopSubmenu;
    return popover;
  }

  private async openNumericRangeFilterModal(categoryKey: string) {
    const popover = await this.openAddFilterPopover();
    await popover.getByTestId(`nested-dropdown-filter-row-${categoryKey}`).click();

    const modal = this.page.getByTestId(`numeric-range-modal-${categoryKey}`);
    await expect(modal).toBeVisible();
    return modal;
  }

  private async openTextareaListFilterModal(categoryKey: string) {
    const popover = await this.openAddFilterPopover();
    await popover.getByTestId(`nested-dropdown-filter-row-${categoryKey}`).click();

    const modal = this.page.getByTestId(`textarea-list-modal-${categoryKey}`);
    await expect(modal).toBeVisible();
    return modal;
  }

  private async dismissAddFilterPopover() {
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");
    if (this.isMobile) {
      const box = await popover.boundingBox();
      const viewport = this.page.viewportSize();

      if (box && viewport) {
        const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);
        const candidates = [
          {
            x: clamp(box.x - 12, 4, viewport.width - 4),
            y: clamp(box.y + 12, 4, viewport.height - 4),
          },
          {
            x: clamp(box.x + box.width + 12, 4, viewport.width - 4),
            y: clamp(box.y + 12, 4, viewport.height - 4),
          },
          {
            x: clamp(box.x + 12, 4, viewport.width - 4),
            y: clamp(box.y - 12, 4, viewport.height - 4),
          },
          {
            x: clamp(box.x + 12, 4, viewport.width - 4),
            y: clamp(box.y + box.height + 12, 4, viewport.height - 4),
          },
        ];

        for (const { x, y } of candidates) {
          await this.page.mouse.click(x, y);
          if (await popover.isHidden().catch(() => true)) {
            return;
          }
        }
      }
    }

    // Desktop keeps the trigger stable while the popover is open, so toggling it remains
    // the most direct close path there. On mobile this is only a fallback when the outside
    // tap candidates above are unavailable.
    await this.page.getByTestId("filter-nested-filters-meta").click();
    await expect(popover).toBeHidden();
  }

  private async filterMinersByModel(minerType: string) {
    const popover = await this.openAddFilterPopover();
    const submenu = await this.openModelSubmenu(popover);
    await this.clickDropdownFilterOption(submenu, [minerType]);
    await this.dismissAddFilterPopover();
  }

  async filterRigMiners() {
    await this.filterMinersByModel(PROTO_RIG_MODEL);
    await this.waitForAntminersToDisappear();
  }

  async applyPowerFilter(min: number | undefined, max: number | undefined) {
    const modal = await this.openNumericRangeFilterModal("power");

    const minInput = modal.getByTestId("numeric-range-power-min");
    const maxInput = modal.getByTestId("numeric-range-power-max");

    await minInput.fill(min === undefined ? "" : String(min));
    await maxInput.fill(max === undefined ? "" : String(max));

    await modal.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(modal).toBeHidden();
  }

  async applySubnetFilter(values: string[]) {
    const modal = await this.openTextareaListFilterModal("subnet");

    await modal.getByTestId("textarea-list-subnet").fill(values.join("\n"));
    await modal.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(modal).toBeHidden();
  }

  async filterAllMinersExceptRig() {
    const popover = await this.openAddFilterPopover();
    const submenu = await this.openModelSubmenu(popover);
    // Nested submenu has no select-all; toggle every non-rig option individually.
    const optionRows = submenu.locator('[data-testid^="filter-option-"]');
    const count = await optionRows.count();
    const skipTestId = `filter-option-${PROTO_RIG_MODEL}`;
    for (let i = 0; i < count; i++) {
      const row = optionRows.nth(i);
      const testId = await row.getAttribute("data-testid");
      if (testId !== skipTestId) await row.click();
    }
    await this.dismissAddFilterPopover();
    await this.waitForRigMinersToDisappear();
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
      .filter({ has: this.page.getByTestId("name").getByText(PROTO_RIG_DISPLAY_NAME, { exact: true }) });
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

  async getMinerColumnText(ipAddress: string, columnTestId: string): Promise<string> {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    const text = await minerRow
      .getByTestId(columnTestId)
      .textContent()
      .catch(() => null);
    return text?.trim() ?? "";
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
    await expect(this.page).toHaveURL(/.*\/fleet\/miners/);
    await this.validateTitle("Fleet");
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

  private singleMinerActionsPopover(): Locator {
    return this.page
      .locator(
        '[data-testid="single-miner-actions-popover-popover"], [data-testid="single-miner-actions-popover-popover-sheet"]',
      )
      .first();
  }

  async clickBlinkLEDsButton() {
    const singleMinerAction = this.singleMinerActionsPopover().getByTestId("blink-leds-popover-button");
    if (await singleMinerAction.isVisible().catch(() => false)) {
      await singleMinerAction.click();
      return;
    }

    const quickAction = this.page.getByTestId("actions-menu-quick-action-blink-leds");
    if (await quickAction.isVisible().catch(() => false)) {
      await quickAction.click();
      return;
    }

    if (await this.tryAction(() => this.page.getByText("Blink LEDs", { exact: true }).click(), 2000)) {
      return;
    }

    if (await this.tryAction(() => this.clickActionsMenuButton(), 2000)) {
      await this.page.getByText("Blink LEDs", { exact: true }).click();
      return;
    }

    throw new Error("Could not find a visible Blink LEDs action in the current miner actions UI.");
  }

  async validateActionBarMinerCount(expectedCount: number) {
    await expect(this.page.getByTestId("action-bar")).toBeVisible();
    if (expectedCount === 1) {
      await expect(this.page.getByTestId("action-bar").getByText("1 miner selected")).toBeVisible();
    } else {
      await expect(this.page.getByTestId("action-bar").getByText(`${expectedCount} miners selected`)).toBeVisible();
    }
  }

  async getSelectedMinersCount(): Promise<number> {
    await expect(this.page.getByTestId("action-bar")).toBeVisible();
    const text = (await this.page.getByTestId("action-bar").textContent()) ?? "";
    const match = text.match(/(\d+) miners? selected/);
    if (!match) {
      throw new Error(`Could not find selected miner count in action bar text: ${text}`);
    }
    return Number(match[1]);
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

  async cancelSingleMinerConfirmationDialog() {
    const dialog = this.page.getByTestId("single-miner-actions-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog).toBeHidden();
  }

  async dismissSingleMinerActionsPopoverIfVisible() {
    const popover = this.singleMinerActionsPopover();
    if (!(await popover.isVisible().catch(() => false))) {
      return;
    }

    await this.page.keyboard.press("Escape").catch(() => undefined);
    if (!(await popover.isVisible().catch(() => false))) {
      return;
    }

    const mobileSheet = this.page.getByTestId("single-miner-actions-popover-popover-sheet");
    if (await mobileSheet.isVisible().catch(() => false)) {
      await mobileSheet.click({ position: { x: 8, y: 8 } });
    }

    if (await popover.isVisible().catch(() => false)) {
      await this.page.mouse.click(8, 8);
    }

    await expect(popover).toBeHidden();
  }

  async clickEditMiningPoolButton() {
    await this.page.getByTestId("mining-pool-popover-button").click();
  }

  async clickUpdateFirmwareButton() {
    await this.page.getByTestId("firmware-update-popover-button").click();
  }

  async validateFirmwareUpdateModalOpened() {
    await this.validateTitleInModal("Add firmware payload");
  }

  async selectExistingFirmwareFile(fileName: string) {
    await this.page.getByRole("radio").filter({ hasText: fileName }).click();
  }

  async clickContinueInFirmwareUpdateModal() {
    await this.clickIn("Continue", "modal");
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

  async clickDownloadLogsButton() {
    await this.page.getByTestId("download-logs-popover-button").click();
  }

  async clickRenameButton() {
    await this.page.getByTestId("rename-popover-button").click();
  }

  async clickUpdateWorkerNameButton() {
    await this.page.getByTestId("update-worker-names-popover-button").click();
  }

  async validateUpdateWorkerNameModalOpened() {
    await this.validateTitleInModal("Update worker name");
  }

  async fillUpdateWorkerNameInput(name: string) {
    await this.page.getByTestId("update-worker-name-input").fill(name);
  }

  async continueUpdateWorkerNameNoChangesIfVisible() {
    const dialog = this.page.getByTestId("update-worker-name-no-changes-dialog");
    try {
      await dialog.waitFor({ state: "visible", timeout: DEFAULT_INTERVAL });
      await dialog.getByRole("button", { name: "Yes, continue", exact: true }).click();
    } catch {
      // Dialog not present, continue
    }
  }

  async clickBulkWorkerNameSave() {
    await this.bulkWorkerNameSaveButton().click();
  }

  async validateBulkWorkerNameModalOpened() {
    await this.validateTitle("Update worker names");
  }

  async validateBulkWorkerNameSaveLabel(expectedLabel: string) {
    await expect(this.bulkWorkerNameSaveButton()).toHaveText(expectedLabel);
  }

  async closeBulkWorkerNameModal() {
    await this.page.getByLabel("Close update worker names").click();
  }

  async continueBulkRenameOverwriteWarningIfVisible() {
    const overwriteDialog = this.page.getByTestId("bulk-rename-overwrite-dialog");
    try {
      await overwriteDialog.waitFor({ state: "visible", timeout: DEFAULT_INTERVAL });
      await overwriteDialog.getByRole("button", { name: "Yes, continue", exact: true }).click();
    } catch {
      // Dialog not present, continue
    }
  }

  async clickManageSecurityButton() {
    await this.page.getByTestId("security-popover-button").click();
  }

  async validateManageSecurityModalOpened() {
    await this.validateTitle("Manage security");
  }

  async clickManageSecurityUpdateButton() {
    await this.page.getByRole("button", { name: "Update", exact: true }).first().click();
  }

  async closeManageSecurityModal() {
    await this.page.getByLabel("Close manage security").click();
  }

  async inputCurrentMinerPassword(password: string) {
    await this.page.locator("#currentPassword").fill(password);
  }

  async inputNewMinerPassword(password: string) {
    await this.page.locator("#newPassword").fill(password);
  }

  async inputConfirmMinerPassword(password: string) {
    await this.page.locator("#confirmPassword").fill(password);
  }

  async clickAddToGroupButton() {
    await this.page.getByTestId("add-to-group-popover-button").click();
  }

  async inputNewGroupName(groupName: string) {
    await this.page.locator("#parent-picker-new-name").fill(groupName);
  }

  async validateMinerGroupName(ipAddress: string, expectedGroupName: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await expect(minerRow.getByTestId("groups")).toContainText(expectedGroupName);
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

  async validateBulkRenamePreviewState(expectedName: string, currentName: string) {
    if (expectedName === currentName) {
      await this.validateBulkRenamePreviewUnchangedPlaceholder();
      return;
    }

    await this.waitForBulkRenamePreviewName(expectedName);
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

  async setBulkRenamePropertyOrder(propertyIds: readonly string[]) {
    const didPersist = await this.page.evaluate((orderedPropertyIds) => {
      const storageKey = "proto-ui-preferences";
      const raw = window.localStorage.getItem(storageKey);
      if (!raw) {
        return false;
      }

      const persisted = JSON.parse(raw);
      const preferences = persisted?.state?.ui?.bulkRenamePreferences;
      const properties = preferences?.properties;
      if (!Array.isArray(properties)) {
        return false;
      }

      const propertyById = new Map(properties.map((property: { id: string }) => [property.id, property]));
      const reorderedProperties = orderedPropertyIds
        .map((propertyId) => propertyById.get(propertyId))
        .filter((property): property is { id: string } => Boolean(property));
      const remainingProperties = properties.filter(
        (property: { id: string }) => !orderedPropertyIds.includes(property.id),
      );

      persisted.state.ui.bulkRenamePreferences = {
        ...preferences,
        properties: [...reorderedProperties, ...remainingProperties],
      };

      window.localStorage.setItem(storageKey, JSON.stringify(persisted));
      return true;
    }, propertyIds);

    expect(didPersist, "Expected bulk rename preferences to be persisted in localStorage").toBe(true);

    await this.reloadPage();
    await this.waitForMinersTitle();
    await this.waitForMinersListToLoad();
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

  async saveCustomPropertyOptions() {
    const desktopSave = this.page.getByTestId("custom-property-options-save-button");
    const mobileSave = this.page.getByTestId("custom-property-options-save-button-mobile");

    if (await desktopSave.isVisible().catch(() => false)) {
      await desktopSave.click();
      return;
    }

    await mobileSave.click();
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
    if (expectedText === "") {
      await expect(this.page.getByTestId("modal")).toContainText("—");
      return;
    }

    await expect(
      this.page.getByTestId("fixed-value-preview-highlighted"),
      `Fixed value preview should show "${expectedText}"`,
    ).toHaveText(expectedText);
  }

  async getFixedValuePreviewText(): Promise<string> {
    const preview = this.page.getByTestId("fixed-value-preview-highlighted");
    const hasPreview = await preview.isVisible().catch(() => false);
    if (hasPreview) {
      return (await preview.innerText()).trim();
    }

    await expect(this.page.getByTestId("modal")).toContainText("—");
    return "";
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
    await this.bulkRenameSaveButton().click();
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
    await this.clickSaveInModal();
  }

  async validateMinerName(ipAddress: string, expectedName: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await expect(minerRow.getByTestId("name")).toContainText(expectedName);
  }

  async getMinerWorkerName(ipAddress: string): Promise<string> {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    return (await minerRow.getByTestId("workerName").innerText()).trim();
  }

  async validateMinerWorkerName(ipAddress: string, expectedWorkerName: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await expect(minerRow.getByTestId("workerName")).toContainText(expectedWorkerName);
  }

  async getMinerWithNonEmptyWorkerName(): Promise<{ ipAddress: string; workerName: string }> {
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();

    for (let i = 0; i < rowCount; i++) {
      const row = rows.nth(i);
      await row.scrollIntoViewIfNeeded();
      const workerName = (await row.getByTestId("workerName").innerText()).trim();

      if (workerName && workerName !== "—") {
        return {
          ipAddress: (await row.getByTestId("ipAddress").innerText()).trim(),
          workerName,
        };
      }
    }

    throw new Error("Expected at least one visible miner with a non-empty worker name");
  }

  async getAuthenticatedMinersWithNonEmptyWorkerNames(
    count: number,
  ): Promise<Array<{ ipAddress: string; workerName: string }>> {
    const allRows = this.page.getByTestId("list-body").locator("tr");
    const authenticatedRows = allRows.filter({
      has: this.page.locator('input[type="checkbox"]:not([disabled])'),
    });

    const authenticatedCount = await authenticatedRows.count();
    const matchingMiners: Array<{ ipAddress: string; workerName: string }> = [];

    for (let i = 0; i < authenticatedCount; i++) {
      const row = authenticatedRows.nth(i);
      await row.scrollIntoViewIfNeeded();
      const workerName = (await row.getByTestId("workerName").innerText()).trim();

      if (workerName && workerName !== "—") {
        matchingMiners.push({
          ipAddress: (await row.getByTestId("ipAddress").innerText()).trim(),
          workerName,
        });
      }

      if (matchingMiners.length === count) {
        return matchingMiners;
      }
    }

    throw new Error(`Expected at least ${count} authenticated miners with non-empty worker names`);
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
      const rowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async validateAllMinersStatus(status: string, expected: boolean = true, timeoutMs: number = PROLONGED_TIMEOUT) {
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
          timeout: timeoutMs,
        });
      } else {
        await expect(statusLocator).not.toContainText(status, {
          timeout: timeoutMs,
        });
      }
    }
  }

  async validateNoMinerWithStatus(status: string, timeoutMs?: number) {
    await this.validateAllMinersStatus(status, false, timeoutMs);
  }

  async validateAllMinersStatusSettled(status: string) {
    await this.waitForColumnValuesToLoad("status");
    // To avoid miner actions hiding some valuable data in screenshots
    await this.uncheckSelectAllCheckbox();
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();
    // Start from last row to avoid extremely long tests due to lazy loading
    for (let i = rowCount - 1; i >= 0; i--) {
      await rows.nth(i).scrollIntoViewIfNeeded();
      const statusCell = rows.nth(i).locator(`//td[@data-testid='status']`);
      const statusIndicator = statusCell.getByTestId("miner-status-indicator");

      await expect(statusCell).toContainText(status, {
        timeout: PROLONGED_TIMEOUT,
      });
      await expect(statusIndicator).toHaveAttribute("data-status", /^(?!pending$).+/, {
        timeout: PROLONGED_TIMEOUT,
      });
    }
  }

  async getMinerStatus(ipAddress: string): Promise<string> {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    return await minerRow.locator(`//td[@data-testid='status']`).innerText();
  }

  async getVisibleMinerStatuses(): Promise<Array<{ ipAddress: string; status: string }>> {
    await this.waitForColumnValuesToLoad("status");
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();
    const visibleMinerStatuses: Array<{ ipAddress: string; status: string }> = [];

    for (let i = 0; i < rowCount; i++) {
      const row = rows.nth(i);
      await row.scrollIntoViewIfNeeded();
      visibleMinerStatuses.push({
        ipAddress: (await row.getByTestId("ipAddress").innerText()).trim(),
        status: (await row.getByTestId("status").innerText()).trim(),
      });
    }

    return visibleMinerStatuses;
  }

  async validateMinerStatus(ipAddress: string, expectedStatus: string) {
    await expect(async () => {
      try {
        const minerRow = await this.getMinerRowByIp(ipAddress);
        const statusCell = minerRow.locator(`//td[@data-testid='status']`);

        await expect(statusCell).toHaveText(expectedStatus, { timeout: DEFAULT_INTERVAL });
      } catch (error) {
        await this.reloadPage();
        const minerRow = await this.getMinerRowByIp(ipAddress);
        const statusCell = minerRow.locator(`//td[@data-testid='status']`);

        await expect(statusCell).toBeVisible();
        throw error;
      }
    }).toPass({ timeout: PROLONGED_TIMEOUT });
  }

  async validateMinerStatusSettled(ipAddress: string, expectedStatus: string, timeoutMs: number = PROLONGED_TIMEOUT) {
    await expect(async () => {
      try {
        const minerRow = await this.getMinerRowByIp(ipAddress);
        const statusCell = minerRow.locator(`//td[@data-testid='status']`);
        const statusIndicator = statusCell.getByTestId("miner-status-indicator");

        await expect(statusCell).toHaveText(expectedStatus, { timeout: DEFAULT_INTERVAL });
        await expect(statusIndicator).toHaveAttribute("data-status", /^(?!pending$).+/, {
          timeout: DEFAULT_INTERVAL,
        });
      } catch (error) {
        await this.reloadPage();
        const minerRow = await this.getMinerRowByIp(ipAddress);
        const statusCell = minerRow.locator(`//td[@data-testid='status']`);

        await expect(statusCell).toBeVisible();
        throw error;
      }
    }).toPass({ timeout: timeoutMs });
  }

  async validateAllMinersIssues(issue: string, expected: boolean = true) {
    await expect(async () => {
      try {
        // To make sure all miners are loaded and we are not missing any issues due to lazy loading
        await this.waitForColumnValuesToLoad("status");
        // To avoid miner actions hiding some valuable data in screenshots
        await this.uncheckSelectAllCheckbox();
        const rows = this.page.getByTestId("list-body").locator("tr");
        const rowCount = await rows.count();
        for (let i = rowCount - 1; i >= 0; i--) {
          await rows.nth(i).scrollIntoViewIfNeeded();
          const issuesLocator = rows.nth(i).locator(`//td[@data-testid='issues']`);

          if (expected) {
            await expect(issuesLocator).toContainText(issue, {
              timeout: DEFAULT_INTERVAL,
            });
          } else {
            await expect(issuesLocator).not.toContainText(issue, {
              timeout: DEFAULT_INTERVAL,
            });
          }
        }
      } catch (error) {
        await this.reloadPage();
        throw error;
      }
    }).toPass({ timeout: PROLONGED_TIMEOUT });
  }

  async validateActionableMinersIssues(issue: string, expected: boolean = true, expectedCount?: number) {
    await expect(async () => {
      try {
        await this.waitForColumnValuesToLoad("status");
        await this.uncheckSelectAllCheckbox();
        const rows = this.page.getByTestId("list-body").locator("tr");
        const rowCount = await rows.count();
        let actionableCount = 0;

        for (let i = rowCount - 1; i >= 0; i--) {
          const row = rows.nth(i);
          await row.scrollIntoViewIfNeeded();
          const checkbox = row.locator('input[type="checkbox"]').first();
          if (await checkbox.isDisabled()) {
            continue;
          }

          actionableCount++;
          const issuesLocator = row.locator(`//td[@data-testid='issues']`);

          if (expected) {
            await expect(issuesLocator).toContainText(issue, {
              timeout: DEFAULT_INTERVAL,
            });
          } else {
            await expect(issuesLocator).not.toContainText(issue, {
              timeout: DEFAULT_INTERVAL,
            });
          }
        }

        if (expectedCount !== undefined) {
          expect(actionableCount).toBe(expectedCount);
        }
      } catch (error) {
        await this.reloadPage();
        throw error;
      }
    }).toPass({ timeout: PROLONGED_TIMEOUT });
  }

  async validateNoMinerWithIssue(issue: string) {
    await this.validateAllMinersIssues(issue, false);
  }

  async validateNoActionableMinerWithIssue(issue: string, expectedCount?: number) {
    await this.validateActionableMinersIssues(issue, false, expectedCount);
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

      // Get temperature text — format is "65.2 °F" or "65.2 °C"
      const temperatureText = await temperatureLocator.innerText();
      const parts = temperatureText.split(" ");
      expect(parts, `Expected temperature text to have value and unit, but got: "${temperatureText}"`).toHaveLength(2);

      // Validate unit - °C/°F
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

  async validateNoResultsEmptyState() {
    await this.page.getByText("No results", { exact: true }).waitFor();
    await expect(this.page.getByText("No results", { exact: true })).toBeVisible();
    await expect(this.page.getByText("Try adjusting or clearing your filters.", { exact: true })).toBeVisible();
    await expect(this.page.getByRole("button", { name: "Clear all filters", exact: true })).toBeVisible();
  }

  async getMinersCount(): Promise<number> {
    const rows = this.page.getByTestId("list-body").locator("tr");
    return await rows.count();
  }

  async hasAnyMinerWithStatus(status: string): Promise<boolean> {
    await this.waitForColumnValuesToLoad("status");
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();

    for (let i = 0; i < rowCount; i++) {
      const statusText = (await rows.nth(i).getByTestId("status").innerText()).trim();
      if (statusText === status) {
        return true;
      }
    }

    return false;
  }

  async getMinerIpAddressByIndex(index: number): Promise<string> {
    const rows = this.page.getByTestId("list-body").locator("tr");
    const row = rows.nth(index);
    return await row.getByTestId("ipAddress").innerText();
  }

  async getMinerIpAddressByStatus(status: string): Promise<string> {
    await this.waitForColumnValuesToLoad("status");
    const rows = this.page.getByTestId("list-body").locator("tr");
    const rowCount = await rows.count();
    const visibleStatuses: string[] = [];

    for (let i = 0; i < rowCount; i++) {
      const row = rows.nth(i);
      const statusText = (await row.getByTestId("status").innerText()).trim();
      if (statusText) {
        visibleStatuses.push(statusText);
      }

      if (statusText === status) {
        return await row.getByTestId("ipAddress").innerText();
      }
    }

    throw new Error(
      `No visible miner with status "${status}". Visible statuses: ${visibleStatuses.join(", ") || "none"}`,
    );
  }

  async getAuthenticatedMinerIpAddressByIndex(index: number): Promise<string> {
    // Filter out rows where the checkbox input is disabled (unauthenticated miners)
    const allRows = this.page.getByTestId("list-body").locator("tr");
    const authenticatedRows = allRows.filter({
      has: this.page.locator('input[type="checkbox"]:not([disabled])'),
    });

    const authenticatedCount = await authenticatedRows.count();
    if (authenticatedCount <= index) {
      throw new Error(`Only ${authenticatedCount} authenticated miners available, cannot get index ${index}`);
    }

    const row = authenticatedRows.nth(index);
    return await row.getByTestId("ipAddress").innerText();
  }

  async openSingleMinerActionsForAuthenticatedMinerWithAction(actionTestId: string): Promise<string> {
    const allRows = this.page.getByTestId("list-body").locator("tr");
    const authenticatedRows = allRows.filter({
      has: this.page.locator('input[type="checkbox"]:not([disabled])'),
    });
    const authenticatedCount = await authenticatedRows.count();
    const checkedMinerIps: string[] = [];

    for (let i = 0; i < authenticatedCount; i++) {
      const row = authenticatedRows.nth(i);
      await row.scrollIntoViewIfNeeded();

      const minerIp = (await row.getByTestId("ipAddress").innerText()).trim();
      checkedMinerIps.push(minerIp);

      await row.getByTestId("single-miner-actions-menu-button").click();
      const popover = this.singleMinerActionsPopover();
      await expect(popover).toBeVisible();

      if ((await popover.getByTestId(actionTestId).count()) > 0) {
        return minerIp;
      }

      await this.dismissSingleMinerActionsPopoverIfVisible();
    }

    throw new Error(
      `No authenticated miner exposed action "${actionTestId}". Checked miners: ${checkedMinerIps.join(", ") || "none"}`,
    );
  }

  async validateMinerNotPresent(ipAddress: string) {
    const minerRow = this.page.getByTestId(`ipAddress`).getByText(ipAddress, { exact: true });
    await expect(minerRow).toBeHidden();
  }

  async clickAddMinersButton() {
    await this.clickButton("Add miners");
  }

  async clickGetStarted() {
    await this.clickButton("Get started");
  }

  private bulkWorkerNameSaveButton(): Locator {
    return this.page.getByTestId(
      this.isMobile ? "bulk-worker-name-save-button-mobile" : "bulk-worker-name-save-button",
    );
  }

  private bulkRenameSaveButton(): Locator {
    return this.page.getByTestId(this.isMobile ? "bulk-rename-save-button-mobile" : "bulk-rename-save-button");
  }

  async clickMinerElementByTestId(ipAddress: string, testId: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await minerRow.getByTestId(testId).click();
  }

  /**
   * Click a miner cell's interactive element and wait for the status modal to open.
   * Targets the button inside the cell (not the td itself) to avoid clicking
   * empty cell padding. Retries if the click doesn't open the modal.
   */
  async clickMinerElementAndExpectModal(ipAddress: string, testId: string, minerName: string) {
    const modalTitle = this.page.locator(
      `//*[@data-testid='modal']//*[contains(@class,'heading')][text()='${minerName} status']`,
    );
    await expect(async () => {
      const minerRow = await this.getMinerRowByIp(ipAddress);
      const cell = minerRow.getByTestId(testId);
      // Click the button inside the cell if one exists, otherwise the cell itself
      const button = cell.locator("button").first();
      const target = (await button.isVisible().catch(() => false)) ? button : cell;
      await target.click();
      await expect(modalTitle).toBeVisible({ timeout: 3000 });
    }).toPass({ timeout: DEFAULT_TIMEOUT });
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

  async validateSingleMinerActionsHidden(testIds: string[]) {
    const popover = this.singleMinerActionsPopover();
    await expect(popover).toBeVisible();

    for (const testId of testIds) {
      await expect(popover.getByTestId(testId), `Expected action "${testId}" to be hidden.`).toHaveCount(0);
    }
  }
}
