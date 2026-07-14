import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";
import { ModalMinerSelectionList } from "./components/modalMinerSelectionList";

export interface RackSelectorMiner {
  ipAddress: string;
  sortName: string;
  model: string;
}

export class RacksPage extends BasePage {
  private readonly modalMinerList = new ModalMinerSelectionList(this.page.getByTestId("modal"));

  async validateRacksPageOpened() {
    await expect(this.page).toHaveURL(/.*\/fleet\/racks/);
    await this.validateTitle("Fleet");
  }

  async validateAddRackButtonHidden() {
    await expect(this.page.getByRole("button", { name: "Add rack", exact: true })).toHaveCount(0);
  }

  async clickAddRackButton() {
    await this.clickButton("Add rack");
    await this.validateTitleInModal("Rack settings");
  }

  async inputZone(zone: string) {
    await this.page.locator("#rack-zone").fill(zone);
  }

  async inputRackLabel(label: string) {
    await this.page.locator("#rack-label").fill(label);
  }

  async enableCustomRackLayout() {
    const columnsInput = this.page.locator("#rack-columns");
    if (!(await columnsInput.isDisabled())) {
      return;
    }

    await this.selectOption("rack-type-select", "New Layout");
  }

  async inputColumns(columns: number | string) {
    await this.page.locator("#rack-columns").fill(String(columns));
  }

  async inputRows(rows: number | string) {
    await this.page.locator("#rack-rows").fill(String(rows));
  }

  async getOrderIndexValue(): Promise<string> {
    const text = await this.page.getByTestId("order-index-select").innerText();
    return text
      .replace(/\s+/g, " ")
      .replace(/^Order index\s*/i, "")
      .trim();
  }

  async clickContinueFromRackSettings() {
    await this.clickIn("Continue", "modal");
  }

  async validateRackSettingsFieldError(
    fieldId: "rack-zone" | "rack-label" | "rack-columns" | "rack-rows",
    message: string,
  ) {
    await expect(this.page.locator(`#${fieldId}-error`)).toHaveText(message);
  }

  async validateRackConfiguration(columns: number, rows: number, orderIndexValue: string) {
    await expect(this.page.getByText(`${columns}x${rows}, ${orderIndexValue}`, { exact: true })).toBeVisible();
  }

  async validateAssignedMinersCount(assigned: number, total: number) {
    await expect(this.page.getByText(`${assigned}/${total} assigned`, { exact: true })).toBeVisible();
  }

  async clickAddMiners() {
    await this.clickButton("Add miners");
    await this.validateTitleInModal("Select miners");
  }

  async clickManageMiners() {
    const overflowTrigger = this.page.getByTestId("overflow-menu-trigger");
    if (this.isMobile && (await overflowTrigger.isVisible().catch(() => false))) {
      await overflowTrigger.click();
    }

    await this.clickButton("Manage Miners");
    await this.validateTitleInModal("Select miners");
  }

  async filterModalType(type: string) {
    // The modal's filters live in a nested "Add filter" popover (matching the
    // fleet miner list). Scope the trigger to the modal so we don't hit the
    // rack-overview page's own filter behind it; the popover/submenu are
    // portaled to the body, so query them page-level.
    const trigger = this.page.getByTestId("modal").getByTestId("filter-nested-filters-meta");
    await trigger.click();
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");
    await expect(popover).toBeVisible();

    await popover.getByTestId("nested-dropdown-filter-row-model").click();
    // Desktop portals a side submenu; phone/tablet collapses the options into the
    // popover with a "back" header.
    const desktopSubmenu = this.page.getByTestId("nested-dropdown-filter-submenu-model");
    const mobileBack = popover.getByTestId("nested-dropdown-filter-back");
    await expect(desktopSubmenu.or(mobileBack)).toBeVisible();
    const submenu = (await desktopSubmenu.isVisible().catch(() => false)) ? desktopSubmenu : popover;

    await this.clickDropdownFilterOption(submenu, type);

    // Checkbox selection applies immediately; toggle the trigger to close the
    // popover so it doesn't overlay the list.
    if (this.isMobile) {
      await this.dismissMobilePopoverSheet("nested-dropdown-filter-popover");
      await expect(popover).toBeHidden();
      return;
    }

    await trigger.click();
    await expect(popover).toBeHidden();
  }

  async waitForMinerSelectorListToLoad() {
    await this.modalMinerList.waitForListToLoad();
  }

  async getAllVisibleMinersFromSelector(): Promise<RackSelectorMiner[]> {
    const rowCount = await this.modalMinerList.getRowCount();
    const miners: RackSelectorMiner[] = [];

    for (let i = 0; i < rowCount; i++) {
      miners.push({
        ipAddress: await this.modalMinerList.getCellTextByIndex(i, "ipAddress"),
        sortName: await this.modalMinerList.getCellTextByIndex(i, "name"),
        model: await this.modalMinerList.getCellTextByIndex(i, "type"),
      });
    }

    return miners;
  }

  async getMinersFromSelector(indexes: number[]): Promise<RackSelectorMiner[]> {
    const miners: RackSelectorMiner[] = [];

    for (const index of indexes) {
      miners.push({
        ipAddress: await this.modalMinerList.getCellTextByIndex(index, "ipAddress"),
        sortName: await this.modalMinerList.getCellTextByIndex(index, "name"),
        model: await this.modalMinerList.getCellTextByIndex(index, "type"),
      });
    }

    return miners;
  }

  async getSelectableMinerIndexes(count: number): Promise<number[]> {
    const indexes = await this.modalMinerList.getSelectableRowIndexes(count);
    expect(indexes).toHaveLength(count);
    return indexes;
  }

  async selectMinersInSelectorByIndex(indexes: number[]) {
    await this.modalMinerList.selectRowsByIndex(indexes);
  }

  async clickSelectAllMinersInSelector() {
    await this.modalMinerList.clickSelectAllCheckbox();
  }

  async toggleMinerInSelectorByIpAddress(ipAddress: string) {
    await this.modalMinerList.selectRowByCellText("ipAddress", ipAddress);
  }

  async clickContinueInMinerSelector() {
    await this.clickIn("Continue", "modal");
  }

  async validateMinerSelectorOverflowError(selectedCount: number, maxSlots: number) {
    await this.validateTextInModal(
      `Cannot add ${selectedCount} miners with only ${maxSlots} available slots. Deselect some miners or update your rack settings.`,
    );
  }

  async clickAssignByName() {
    await this.clickButton("Assign by name");
  }

  async clickAssignByNetwork() {
    await this.clickButton("Assign by network");
  }

  async clickAssignManually() {
    await this.clickButton("Assign manually");
  }

  async selectRackMiner(ipAddress: string) {
    await this.clickMinerRow(ipAddress);
  }

  async clickRackSlot(slotNumber: number) {
    await this.getRackSlot(slotNumber).click();
  }

  async clickRackSlotMenuItem(menuItemLabel: "Search miners" | "Select from list") {
    if (this.isMobile) {
      const actionTestId =
        menuItemLabel === "Search miners" ? "rack-slot-search-miners-action" : "rack-slot-select-from-list-action";
      await this.page.getByTestId("rack-slot-actions-sheet-content").getByTestId(actionTestId).click();
      return;
    }

    await this.page.getByRole("menuitem", { name: menuItemLabel, exact: true }).click();
  }

  async assignSearchMinerByIpAddress(ipAddress: string) {
    await this.validateTitleInModal("Search miners");
    await this.modalMinerList.waitForListToLoad();
    await this.modalMinerList.selectRowByCellText("ipAddress", ipAddress);
    await this.clickIn("Assign", "modal");
    await this.validateTitleInModalNotVisible("Search miners");
  }

  async validateMinersAssignedByName(miners: readonly RackSelectorMiner[]) {
    const expectedPositions = this.getExpectedPositionsForAssignByName(miners);

    for (let i = 0; i < miners.length; i++) {
      const minerRow = this.getAssignedMinerRow(miners[i].ipAddress);
      await expect(minerRow.getByTestId("checkmark-icon")).toBeVisible();
      await expect(minerRow.getByTestId("rack-miner-position")).toHaveText(
        `Position ${String(expectedPositions[i]).padStart(2, "0")}`,
      );
    }

    await this.validateRackSlotsHighlighted(expectedPositions);
  }

  async validateMinersAssignedByNetwork(miners: readonly RackSelectorMiner[]) {
    const sortedMiners = this.getMinersSortedByIpAddress(miners);

    for (let i = 0; i < sortedMiners.length; i++) {
      const row = this.getAssignedMinerRowByPosition(i + 1);
      await expect(row.getByTestId("rack-miner-name")).toHaveText(sortedMiners[i].sortName);
      await expect(row.getByTestId("rack-miner-subtitle")).toContainText(sortedMiners[i].ipAddress);
    }

    await this.validateRackSlotsHighlighted(sortedMiners.map((_, index) => index + 1));
  }

  async assignMinersToSlotsInDomOrder(miners: readonly RackSelectorMiner[]) {
    for (let i = 0; i < miners.length; i++) {
      await this.clickMinerRow(miners[i].ipAddress);
      await this.clickRackSlotByDomIndex(i);
    }
  }

  async validateRackSlotNumbersInDomOrder(expectedNumbers: readonly number[]) {
    const expectedTexts = expectedNumbers.map((value) => String(value).padStart(2, "0"));
    await expect(this.page.locator('[data-testid^="rack-slot-"] span.font-medium')).toHaveText(expectedTexts);
  }

  async validateMinerPositions(miners: readonly RackSelectorMiner[], expectedPositions: readonly number[]) {
    for (let i = 0; i < miners.length; i++) {
      await this.validateMinerRowPosition(miners[i].ipAddress, expectedPositions[i]);
    }
  }

  async validateMinerRowHasGreenCheck(ipAddress: string) {
    const minerRow = this.getAssignedMinerRow(ipAddress);
    await expect(minerRow.getByTestId("checkmark-icon")).toBeVisible();
  }

  async validateMinerRowUnassigned(ipAddress: string) {
    const minerRow = this.getAssignedMinerRow(ipAddress);
    await expect(minerRow.getByTestId("checkmark-icon")).toHaveCount(0);
    await expect(minerRow.getByTestId("rack-miner-position")).toHaveCount(0);
  }

  async validateMinerRowPosition(ipAddress: string, position: number) {
    const minerRow = this.getAssignedMinerRow(ipAddress);
    await expect(minerRow).toContainText(`Position ${String(position).padStart(2, "0")}`);
  }

  async validateRackSlotsHighlighted(slotNumbers: readonly number[]) {
    for (const slotNumber of slotNumbers) {
      const slot = this.getRackSlot(slotNumber);
      await expect(slot).toHaveAttribute("data-slot-state", "assigned");
    }
  }

  async validateRackSlotsNotHighlighted(slotNumbers: readonly number[]) {
    for (const slotNumber of slotNumbers) {
      const slot = this.getRackSlot(slotNumber);
      await expect(slot).toHaveAttribute("data-slot-state", "empty");
    }
  }

  async clickClearAssignments() {
    await this.page.getByRole("button", { name: "Clear", exact: true }).click();
  }

  async clickSaveRack() {
    await this.clickButton("Save");
  }

  async clickViewMiners() {
    const directButton = this.page.getByTestId("rack-page-view-miners");
    if (await directButton.isVisible().catch(() => false)) {
      await directButton.click();
    } else {
      await this.page.getByRole("button", { name: "Device set actions", exact: true }).click();
      const actionsPopover = this.page.getByTestId("group-actions-popover");
      await expect(actionsPopover).toBeVisible();
      await actionsPopover.getByRole("button", { name: "View miners", exact: true }).click();
    }

    await expect(this.page).toHaveURL(/.*\/miners/);
  }

  async clickEditRackSettings() {
    const overflowTrigger = this.page.getByTestId("overflow-menu-trigger");
    if (this.isMobile && (await overflowTrigger.isVisible().catch(() => false))) {
      await overflowTrigger.click();
    }

    await this.clickButton("Edit Rack Settings");
    await this.validateTitleInModal("Rack settings");
  }

  async changeOrderIndexAndContinue(orderIndexLabel: string) {
    await this.selectOption("order-index-select", orderIndexLabel);
    await this.clickContinueFromRackSettings();
  }

  async validateRackToast(label: string, action: "created" | "updated" = "created") {
    await this.validateTextInToast(`Rack "${label}" ${action}`);
  }

  async validateRackCardVisible(label: string, zone: string) {
    await expect(this.getRackCard(label, zone)).toBeVisible();
  }

  async validateRackCardGrid(label: string, zone: string, columns: number, rows: number) {
    const rackCard = this.getRackCard(label, zone);
    const miniGridCells = rackCard.getByTestId("rack-card-grid").getByTestId("rack-card-slot");
    await expect(miniGridCells).toHaveCount(columns * rows);
  }

  async openRackCard(label: string, zone: string) {
    await this.getRackCard(label, zone).click();
  }

  async clickViewList(timeout: number = DEFAULT_TIMEOUT) {
    await this.clickRackViewMode("View list", timeout);
  }

  async clickViewGrid(timeout: number = DEFAULT_TIMEOUT) {
    await this.clickRackViewMode("View grid", timeout);
  }

  private async clickRackViewMode(label: "View grid" | "View list", timeout: number) {
    const controls = this.page.getByTestId("segmented-control");
    let visibleControlIndex = -1;

    await expect
      .poll(
        async () => {
          const count = await controls.count();

          for (let i = 0; i < count; i++) {
            const control = controls.nth(i);
            const button = control.getByRole("button", { name: label, exact: true });

            if ((await control.isVisible().catch(() => false)) && (await button.isVisible().catch(() => false))) {
              visibleControlIndex = i;
              return "visible";
            }
          }

          return "hidden";
        },
        {
          timeout,
          intervals: [DEFAULT_INTERVAL],
          message: `Expected the ${label} segmented control button to be visible.`,
        },
      )
      .toBe("visible");

    await controls.nth(visibleControlIndex).getByRole("button", { name: label, exact: true }).click();
  }

  async applyZoneFilter(zoneNames: string[]) {
    await this.setNestedCheckboxFilterSelection("zone", zoneNames);
  }

  async applySiteFilter(siteNames: string[]) {
    await this.setNestedCheckboxFilterSelection("site", siteNames);
  }

  async applyBuildingFilter(buildingNames: string[]) {
    await this.setNestedCheckboxFilterSelection("building", buildingNames);
  }

  async toggleAllZoneFilters() {
    await this.toggleAllNestedCheckboxFilterOptions("zone");
  }

  async selectGridSort(sortLabel: string) {
    await this.clickVisibleFilterDropdown("Sort");
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await this.clickDropdownFilterOption(popover, sortLabel);
    if (await popover.isVisible().catch(() => false)) {
      if (this.isMobile) {
        await this.dismissMobilePopoverSheet("dropdown-filter-popover");
        await expect(popover).toBeHidden();
        return;
      }

      await this.clickVisibleFilterDropdown("Sort");
    }
    await expect(popover).toBeHidden();
  }

  async waitForRackListToLoad({
    allowEmpty = true,
    requireManageAccess = true,
    timeout = DEFAULT_TIMEOUT,
  }: {
    allowEmpty?: boolean;
    requireManageAccess?: boolean;
    timeout?: number;
  } = {}) {
    if (requireManageAccess) {
      await expect(this.page.getByRole("button", { name: "Add rack" }).first()).toBeVisible({ timeout });
    }

    const rows = this.page.getByTestId("list-row");
    const noRowsText = this.page.getByText("You haven't set up any racks");

    if (!allowEmpty) {
      await expect(rows).not.toHaveCount(0);
    }

    await expect(async () => {
      const isEmptyStateVisible = await noRowsText.isVisible().catch(() => false);
      if (isEmptyStateVisible) {
        return;
      }

      const rowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout, intervals: [DEFAULT_INTERVAL] });
  }

  async listRackNames(timeout: number = DEFAULT_TIMEOUT): Promise<string[]> {
    await this.waitForRackListToLoad({ timeout });

    const nameCells = this.page.getByTestId("list-row").getByTestId("name");
    const count = await nameCells.count();
    const names: string[] = [];

    for (let i = 0; i < count; i++) {
      names.push((await nameCells.nth(i).innerText()).trim());
    }

    return names;
  }

  async getGridRackLabels(): Promise<string[]> {
    const labels = this.page.locator('[data-testid="rack-card-label"]:visible');
    return (await labels.allTextContents()).map((label) => label.trim()).filter(Boolean);
  }

  async validateRackRow(label: string, zone: string, miners: number) {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("zone")).toHaveText(zone);
    await expect(row.getByTestId("miners")).toHaveText(String(miners));
  }

  async validateRackMinerCount(label: string, miners: number) {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("miners")).toHaveText(String(miners));
  }

  async validateRackPlacementRow(label: string, siteName: string, buildingName = "") {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("site")).toHaveText(siteName);
    await expect(row.getByTestId("building")).toHaveText(buildingName);
  }

  async validateRackNotVisible(label: string) {
    await expect(this.getRackListRow(label)).toHaveCount(0);
  }

  async openRackFromList(label: string) {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();
    await row.getByTestId("name").getByRole("button", { name: label, exact: true }).click();
  }

  async getRackIdByLabel(label: string): Promise<bigint> {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();

    const trigger = row.getByRole("button", { name: `Actions for ${label}`, exact: true });
    await expect(trigger).toBeVisible();

    const testId = await trigger.getAttribute("data-testid");
    const capturedId = testId?.match(/^rack-list-row-(\d+)-actions-trigger$/)?.[1];
    if (!capturedId) {
      throw new Error(`Could not parse rack id from row action trigger: ${testId ?? "missing test id"}`);
    }

    return BigInt(capturedId);
  }

  async assignRackToBuildingFromList(label: string, buildingName: string) {
    await this.navigateToRacksPage();
    await this.clickViewList();
    await this.waitForRackListToLoad({ allowEmpty: false });
    await this.openRackActionsFromList(label);
    await this.page.getByText("Add to building", { exact: true }).click();
    await this.selectParentPickerTarget(buildingName);
  }

  async deleteRackByLabelIfVisible(label: string) {
    await this.navigateToRacksPage();
    await this.tryAction(() => this.clickViewList());
    if (!(await this.tryAction(() => this.openRackFromList(label)))) {
      return;
    }

    await this.clickEditRack();
    await this.clickDeleteRack();
    await this.clickDeleteConfirm();
    await this.tryAction(() => this.validateRackDeletedToast());
  }

  async clickEditRack() {
    await this.clickButton("Edit rack");
  }

  async openRackOverviewActionsMenu() {
    await this.page.getByLabel("Device set actions").click();
    await expect(this.page.getByTestId("group-actions-popover")).toBeVisible();
  }

  async clickRackOverviewManagePower() {
    await this.page.getByTestId("manage-power-popover-button").click();
    await this.validateTitleInModal("Manage power");
  }

  async clickRackOverviewAssignPools() {
    await this.page.getByTestId("mining-pool-popover-button").click();
  }

  async clickRackOverviewManageSecurity() {
    await this.page.getByTestId("security-popover-button").click();
  }

  async clickDeleteRack() {
    const overflowTrigger = this.page.getByTestId("overflow-menu-trigger");
    if (this.isMobile && (await overflowTrigger.isVisible().catch(() => false))) {
      await overflowTrigger.click();
    }

    await this.clickButton("Delete Rack");
  }

  async clickDeleteConfirm() {
    await this.clickButton("Delete");
  }

  async validateRackDeletedToast(timeout: number = DEFAULT_TIMEOUT) {
    await this.validateTextInToast("Rack deleted", timeout);
  }

  async validateRackOverviewAssignedSlots(slotNumbers: readonly number[]) {
    for (const slotNumber of slotNumbers) {
      const slot = this.getRackOverviewSlot(slotNumber);
      await expect(slot).not.toHaveAttribute("data-slot-state", "empty");
      await expect(slot.getByTestId("rack-detail-slot-empty-action")).toHaveCount(0);

      const slotNumberLabel = slot.getByTestId("rack-detail-slot-number");
      if ((await slotNumberLabel.count()) > 0) {
        await expect(slotNumberLabel).toHaveText(String(slotNumber).padStart(2, "0"));
      }
    }
  }

  async validateRackOverviewEmptySlots(slotNumbers: readonly number[]) {
    for (const slotNumber of slotNumbers) {
      const slot = this.getRackOverviewSlot(slotNumber);
      await expect(slot).toHaveAttribute("data-slot-state", "empty");
      await expect(slot.getByTestId("rack-detail-slot-empty-action")).toBeVisible();
    }
  }

  async clickRackOverviewEmptySlot(slotNumber: number) {
    await this.getRackOverviewSlot(slotNumber).getByTestId("rack-detail-slot-empty-action").click();
    await this.validateTitleInModal("Search miners");
  }

  private async selectOption(testId: string, optionLabel: string) {
    await this.page.getByTestId(testId).click();
    await this.page.getByRole("option", { name: optionLabel, exact: true }).click();
  }

  private async clickDropdownFilterOption(popover: Locator, optionName: string) {
    const optionByTestId = popover.getByTestId(`filter-option-${optionName}`);
    const optionByTestIdCount = await optionByTestId.count();
    for (let i = 0; i < optionByTestIdCount; i++) {
      const option = optionByTestId.nth(i);
      if (await option.isVisible().catch(() => false)) {
        await option.click();
        return;
      }
    }

    const optionRows = popover.locator('[data-testid^="filter-option-"]').filter({ hasText: optionName });
    const optionRowCount = await optionRows.count();
    for (let i = 0; i < optionRowCount; i++) {
      const option = optionRows.nth(i);
      const label = ((await option.textContent()) ?? "").replace(/\s+/g, " ").trim();
      if (label === optionName && (await option.isVisible().catch(() => false))) {
        await option.click();
        return;
      }
    }

    throw new Error(`Could not find a visible dropdown filter option named "${optionName}".`);
  }

  private async clickVisibleFilterDropdown(title: string) {
    if (this.isMobile) {
      await this.dismissMobilePopoverSheet("dropdown-filter-popover");
    }

    const dropdowns = this.page.getByTestId(`filter-dropdown-${title}`);
    const count = await dropdowns.count();

    for (let i = 0; i < count; i++) {
      const dropdown = dropdowns.nth(i);
      if (await dropdown.isVisible().catch(() => false)) {
        await dropdown.click();
        return;
      }
    }

    throw new Error(`No visible ${title} filter dropdown found`);
  }

  private getAssignedMinerRow(ipAddress: string): Locator {
    return this.page.getByTestId("rack-miner-row").filter({ hasText: ipAddress }).first();
  }

  private getAssignedMinerRowByPosition(position: number): Locator {
    return this.page
      .getByTestId("rack-miner-row")
      .filter({
        has: this.page
          .getByTestId("rack-miner-position")
          .getByText(`Position ${String(position).padStart(2, "0")}`, { exact: true }),
      })
      .first();
  }

  private async clickMinerRow(ipAddress: string) {
    await this.getAssignedMinerRow(ipAddress).click();
  }

  private async clickRackSlotByDomIndex(index: number) {
    await this.page.locator('[data-testid^="rack-slot-"]').nth(index).click();
  }

  private getRackSlot(slotNumber: number): Locator {
    return this.page.getByTestId(new RegExp(`^rack-slot-0*${slotNumber}$`));
  }

  private getRackCard(label: string, zone: string): Locator {
    return this.page.getByTestId("rack-card").filter({ hasText: label }).filter({ hasText: zone }).first();
  }

  private getRackOverviewSlot(slotNumber: number): Locator {
    return this.page.getByTestId(`rack-detail-slot-${String(slotNumber).padStart(2, "0")}`);
  }

  private getExpectedPositionsForAssignByName(miners: readonly RackSelectorMiner[]): number[] {
    const sortedMiners = [...miners].sort((left, right) => left.sortName.localeCompare(right.sortName));
    return miners.map((miner) => sortedMiners.findIndex((candidate) => candidate.ipAddress === miner.ipAddress) + 1);
  }

  private getMinersSortedByIpAddress(miners: readonly RackSelectorMiner[]): RackSelectorMiner[] {
    const padIp = (ipAddress: string) => ipAddress.replace(/\d+/g, (octet) => octet.padStart(3, "0"));
    return [...miners].sort((left, right) => padIp(left.ipAddress).localeCompare(padIp(right.ipAddress)));
  }

  private getRackListRow(label: string): Locator {
    return this.page
      .getByTestId("list-row")
      .filter({ has: this.page.getByTestId("name").getByRole("button", { name: label, exact: true }) })
      .first();
  }

  private async openRackActionsFromList(label: string) {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: `Actions for ${label}`, exact: true }).click();
  }

  private async selectParentPickerTarget(label: string) {
    const modal = this.page.getByTestId("modal");
    await expect(modal).toBeVisible();
    await modal.getByText(label, { exact: true }).click();
    const saveButton = modal.getByRole("button", { name: "Save", exact: true });
    await expect(saveButton).toBeVisible();
    await saveButton.click();
    await expect(modal).toHaveCount(0);
  }
}
