import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";
import { ModalMinerSelectionList } from "./components/modalMinerSelectionList";

const EMPTY_GROUP_PLACEHOLDER = "—";

export class GroupsPage extends BasePage {
  private readonly modalMinerList = new ModalMinerSelectionList(this.page.getByTestId("modal"));

  private async clickLocator(locator: Locator) {
    try {
      await locator.click({ timeout: 2000 });
    } catch {
      await locator.evaluate((node) => {
        (node as HTMLElement).click();
      });
    }
  }

  async waitForSavedGroupsListToLoad() {
    const rows = this.page.getByTestId("list-row");

    await expect(this.page.getByRole("button", { name: "Add group" })).toBeVisible();
    await expect(async () => {
      const rowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  private async clickDropdownFilterOption(popover: Locator, optionNames: string[]) {
    for (const optionName of optionNames) {
      const optionByTestId = popover.getByTestId(`filter-option-${optionName}`).first();
      if (await optionByTestId.isVisible().catch(() => false)) {
        await optionByTestId.evaluate((node) => {
          node.scrollIntoView({ block: "center", inline: "nearest" });
        });
        await this.clickLocator(optionByTestId);
        return;
      }

      const optionByText = popover.getByText(optionName, { exact: true }).first();
      if (await optionByText.isVisible().catch(() => false)) {
        await optionByText.evaluate((node) => {
          node.scrollIntoView({ block: "center", inline: "nearest" });
        });
        await this.clickLocator(optionByText);
        return;
      }
    }

    throw new Error(`Unable to find filter option. Tried: ${optionNames.join(", ")}`);
  }

  async clickAddGroupButton() {
    await this.clickButton("Add group");
    await this.validateModalIsOpen();
  }

  async closeModal() {
    await this.page.getByTestId("modal").getByTestId("header-icon-button").click();
    await this.validateModalIsClosed();
  }

  async openSavedGroup(groupName: string) {
    const groupRow = this.getGroupRow(groupName);
    await expect(groupRow).toBeVisible();

    await groupRow.getByLabel("Device set actions").click();
    await this.clickButton("Edit group");
    await this.validateModalIsOpen();
  }

  async openSavedGroupOverview(groupName: string) {
    const groupRow = this.getGroupRow(groupName);
    await expect(groupRow).toBeVisible();
    await groupRow.getByTestId("name").getByRole("link", { name: groupName, exact: true }).click();
    const expectedPath = `/groups/${encodeURIComponent(groupName)}`;
    await expect(this.page).toHaveURL((url) => url.pathname === expectedPath);
    await this.validateTitle(groupName);
  }

  async openSavedGroupOverviewFromActionsMenu(groupName: string) {
    const groupRow = this.getGroupRow(groupName);
    const expectedPath = `/groups/${encodeURIComponent(groupName)}`;

    await expect(groupRow).toBeVisible();
    await groupRow.getByLabel("Device set actions").click();
    await this.page.getByTestId("view-group-popover-button").click();
    await expect(this.page).toHaveURL((url) => url.pathname === expectedPath);
    await this.validateTitle(groupName);
  }

  async returnToGroupsListFromOverview() {
    await this.page.getByLabel("Back to groups").click();
    await this.waitForSavedGroupsListToLoad();
  }

  async inputGroupName(groupName: string) {
    await this.page.locator(`//input[@id='group-name']`).fill(groupName);
  }

  async clickSelectAllCheckboxInModal() {
    await this.modalMinerList.clickSelectAllCheckbox();
  }

  async waitForModalListToLoad() {
    await this.modalMinerList.waitForListToLoad();
  }

  async getModalListRowCount(): Promise<number> {
    return await this.modalMinerList.getRowCount();
  }

  async selectMinersByIndex(indexes: number[]) {
    await this.modalMinerList.selectRowsByIndex(indexes);
  }

  async validateMinerGroupsByIndex(index: number, expectedGroups: string) {
    const groupCell = this.page.getByTestId("modal").getByTestId("list-row").nth(index).getByTestId("group");
    await expect(groupCell).toHaveText(expectedGroups);
  }

  async getModalRowGroupByIndex(index: number): Promise<string> {
    const groupText = await this.modalMinerList.getCellTextByIndex(index, "group");
    return groupText === EMPTY_GROUP_PLACEHOLDER ? "" : groupText;
  }

  async getModalRowIpAddressByIndex(index: number): Promise<string> {
    return await this.modalMinerList.getCellTextByIndex(index, "ipAddress");
  }

  async getUngroupedMinerIps(limit: number): Promise<string[]> {
    const rowCount = await this.getModalListRowCount();
    const minerIps: string[] = [];

    for (let i = 0; i < rowCount && minerIps.length < limit; i++) {
      if ((await this.getModalRowGroupByIndex(i)) !== "") {
        continue;
      }
      minerIps.push(await this.getModalRowIpAddressByIndex(i));
    }

    return minerIps;
  }

  async selectMinerByIp(ipAddress: string) {
    await this.modalMinerList.selectRowByCellText("ipAddress", ipAddress);
  }

  async validateMinerGroupsByIp(ipAddress: string, expectedGroups: string) {
    const groupCell = this.page
      .getByTestId("modal")
      .getByTestId("list-row")
      .filter({ has: this.page.getByTestId("ipAddress").getByText(ipAddress, { exact: true }) })
      .first()
      .getByTestId("group");
    await expect(groupCell).toHaveText(expectedGroups);
  }

  async getModalVisibleIpAddresses(): Promise<string[]> {
    return await this.modalMinerList.getVisibleCellTexts("ipAddress");
  }

  async validateOnlyTheseIpsVisibleInModal(expectedIps: string[]) {
    const visibleIps = await this.getModalVisibleIpAddresses();
    expect(visibleIps).toHaveLength(expectedIps.length);
    const expectedSet = new Set(expectedIps);
    for (const ip of visibleIps) {
      expect(expectedSet.has(ip)).toBe(true);
    }
  }

  // The modal's filters live in a nested "Add filter" popover (matching the
  // fleet miner list). The trigger is scoped to the modal; the popover/submenu
  // are portaled to the body.
  private async openModalFilterSubmenu(categoryKey: string, clearFirst = false) {
    const trigger = this.page.getByTestId("modal").getByTestId("filter-nested-filters-meta");
    await this.clickLocator(trigger);
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");
    await expect(popover).toBeVisible();

    if (clearFirst) {
      // Checkbox facets accumulate, so callers that expect one selection to
      // replace the last must clear first. "Clear all" only renders when
      // something is selected, and clicking it closes the popover.
      const clearAll = popover.getByRole("button", { name: "Clear all" });
      if (await clearAll.isVisible().catch(() => false)) {
        await clearAll.click();
        await expect(popover).toBeHidden();
        await this.clickLocator(trigger);
        await expect(popover).toBeVisible();
      }
    }

    await popover.getByTestId(`nested-dropdown-filter-row-${categoryKey}`).click();
    const desktopSubmenu = this.page.getByTestId(`nested-dropdown-filter-submenu-${categoryKey}`);
    const mobileBack = popover.getByTestId("nested-dropdown-filter-back");
    await expect(desktopSubmenu.or(mobileBack)).toBeVisible();
    const submenu = (await desktopSubmenu.isVisible().catch(() => false)) ? desktopSubmenu : popover;
    return { trigger, popover, submenu };
  }

  async filterModalType(type: string) {
    const { trigger, popover, submenu } = await this.openModalFilterSubmenu("model");
    await this.clickDropdownFilterOption(submenu, [type]);
    await this.clickLocator(trigger);
    await expect(popover).toBeHidden();
  }

  async filterModalGroup(groupName: string) {
    // Each call replaces the previously-filtered group (see the group-filter
    // validation spec), so clear the prior selection first.
    const { trigger, popover, submenu } = await this.openModalFilterSubmenu("group", true);
    await this.clickDropdownFilterOption(submenu, [groupName]);
    await this.clickLocator(trigger);
    await expect(popover).toBeHidden();
  }

  async clickDeleteGroupInModal() {
    await this.clickButton("Delete group");
  }

  async clickDeleteConfirm() {
    await this.clickButton("Delete");
  }

  async validateErrorMessage(text: string) {
    await expect(this.page.getByTestId("error-msg")).toHaveText(text);
  }

  async validateSavedGroupVisible(groupName: string) {
    await expect(this.getGroupRow(groupName)).toBeVisible();
  }

  async validateSavedGroupNotVisible(groupName: string) {
    await expect(this.getGroupRow(groupName)).toBeHidden();
  }

  async validateSavedGroupMinerCount(groupName: string, minerCount: number) {
    await expect(this.getGroupRow(groupName).getByTestId("miners")).toHaveText(`${minerCount}`);
  }

  async getSavedGroupCount(): Promise<number> {
    const rows = this.page.getByTestId("list-row");
    return await rows.count();
  }

  async listSavedGroupNames(): Promise<string[]> {
    await this.waitForSavedGroupsListToLoad();

    const nameCells = this.page.getByTestId("list-row").getByTestId("name");
    const count = await nameCells.count();
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      names.push((await nameCells.nth(i).innerText()).trim());
    }
    return names;
  }

  async deleteSavedGroupIfVisible(groupName: string) {
    const groupRow = this.getGroupRow(groupName);
    if (!(await groupRow.isVisible().catch(() => false))) {
      return;
    }

    await this.openSavedGroup(groupName);
    await this.clickDeleteGroupInModal();
    await this.clickDeleteConfirm();
    await this.validateSavedGroupNotVisible(groupName);
  }

  private getGroupRow(groupName: string) {
    return this.page
      .getByTestId("list-row")
      .filter({ has: this.page.getByTestId("name").getByText(groupName, { exact: true }) })
      .first();
  }

  async clickGroupActionsButton(groupName: string) {
    const groupRow = this.getGroupRow(groupName);
    await expect(groupRow).toBeVisible();
    await groupRow.getByLabel("Device set actions").click();
  }

  async openGroupOverviewActionsMenu() {
    await this.page.getByLabel("Device set actions").click();
    await expect(this.page.getByTestId("group-actions-popover")).toBeVisible();
  }

  async clickGroupOverviewManagePower() {
    await this.page.getByTestId("manage-power-popover-button").click();
    await this.validateTitleInModal("Manage power");
  }

  async clickGroupOverviewAssignPools() {
    await this.page.getByTestId("mining-pool-popover-button").click();
  }

  async clickGroupOverviewManageSecurity() {
    await this.page.getByTestId("security-popover-button").click();
  }

  async clickRebootGroupButton() {
    await this.page.getByTestId("reboot-popover-button").click();
  }

  async validateRebootConfirmationModal(minerCount: number) {
    await this.validateTitle(`Reboot ${minerCount} miners?`);
  }

  async clickRebootConfirm() {
    await this.clickButton("Reboot");
  }
}
