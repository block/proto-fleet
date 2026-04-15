import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";
import { ModalMinerSelectionList } from "./components/modalMinerSelectionList";

export interface RackSelectorMiner {
  ipAddress: string;
  sortName: string;
}

export class RacksPage extends BasePage {
  private readonly modalMinerList = new ModalMinerSelectionList(this.page.getByTestId("modal"));

  async validateRacksPageOpened() {
    await this.validateTitle("Racks");
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

  async getGeneratedRackLabel(): Promise<string> {
    return await this.page.locator("#rack-label").inputValue();
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
    await this.clickButton("View miners");
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

  async clickViewList() {
    await this.clickButton("View list");
  }

  async clickViewGrid() {
    await this.clickButton("View grid");
  }

  async applyZoneFilter(zoneNames: string[]) {
    await this.clickVisibleFilterDropdown("Zone");
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();

    await popover.getByRole("button", { name: "Reset", exact: true }).click();

    for (const zoneName of zoneNames) {
      await this.clickDropdownFilterOption(popover, zoneName);
    }

    await popover.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(popover).toBeHidden();
  }

  async toggleAllZoneFilters() {
    await this.clickVisibleFilterDropdown("Zone");
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await popover.getByText("Select all", { exact: true }).click();
    await popover.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(popover).toBeHidden();
  }

  async selectGridSort(sortLabel: string) {
    await this.clickVisibleFilterDropdown("Sort");
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await this.clickDropdownFilterOption(popover, sortLabel);
    if (await popover.isVisible().catch(() => false)) {
      await this.clickVisibleFilterDropdown("Sort");
    }
    await expect(popover).toBeHidden();
  }

  async waitForRackListToLoad({ allowEmpty = true }: { allowEmpty?: boolean } = {}) {
    await expect(this.page.getByRole("button", { name: "Add rack" }).first()).toBeVisible();

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
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async listRackNames(): Promise<string[]> {
    await this.waitForRackListToLoad();

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

  async openRackFromList(label: string) {
    const row = this.getRackListRow(label);
    await expect(row).toBeVisible();
    await row.getByTestId("name").getByRole("button", { name: label, exact: true }).click();
  }

  async clickEditRack() {
    await this.clickButton("Edit rack");
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

  async validateRackDeletedToast() {
    await this.validateTextInToast("Rack deleted");
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
    const optionByTestId = popover.getByTestId(`filter-option-${optionName}`).first();
    if (await optionByTestId.isVisible().catch(() => false)) {
      await optionByTestId.click();
      return;
    }

    await popover.getByText(optionName, { exact: true }).first().click();
  }

  private async clickVisibleFilterDropdown(title: string) {
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
}
