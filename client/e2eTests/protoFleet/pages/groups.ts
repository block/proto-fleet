import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class GroupsPage extends BasePage {
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

    await groupRow.getByLabel("Group actions").click();
    await this.clickButton("Edit group");
    await this.validateModalIsOpen();
  }

  async inputGroupName(groupName: string) {
    await this.page.locator(`//input[@id='group-name']`).fill(groupName);
  }

  async clickSelectAllCheckboxInModal() {
    await this.page.getByTestId("modal").getByTestId("select-all-checkbox").locator('input[type="checkbox"]').click();
  }

  async waitForModalListToLoad() {
    const rows = this.page.getByTestId("modal").getByTestId("list-body").locator("tr");
    await expect(rows).not.toHaveCount(0);
    await expect(async () => {
      const rowCount = await rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async getModalListRowCount(): Promise<number> {
    const rows = this.page.getByTestId("modal").getByTestId("list-row");
    return await rows.count();
  }

  async selectMinersByIndex(indexes: number[]) {
    const rows = this.page.getByTestId("modal").getByTestId("list-row");
    for (const index of indexes) {
      const row = rows.nth(index);
      await row.scrollIntoViewIfNeeded();
      await row.getByTestId("checkbox").locator('input[type="checkbox"]').click();
    }
  }

  async validateMinerGroupsByIndex(index: number, expectedGroups: string) {
    const groupCell = this.page.getByTestId("modal").getByTestId("list-row").nth(index).getByTestId("group");
    await expect(groupCell).toHaveText(expectedGroups);
  }

  async getModalRowIpAddressByIndex(index: number): Promise<string> {
    const ipCell = this.page.getByTestId("modal").getByTestId("list-row").nth(index).getByTestId("ipAddress");
    return (await ipCell.innerText()).trim();
  }

  async getModalVisibleIpAddresses(): Promise<string[]> {
    const ipCells = this.page.getByTestId("modal").getByTestId("list-row").getByTestId("ipAddress");
    const count = await ipCells.count();
    const result: string[] = [];
    for (let i = 0; i < count; i++) {
      result.push(((await ipCells.nth(i).innerText()) || "").trim());
    }
    return result;
  }

  async validateOnlyTheseIpsVisibleInModal(expectedIps: string[]) {
    const visibleIps = await this.getModalVisibleIpAddresses();
    expect(visibleIps).toHaveLength(expectedIps.length);
    const expectedSet = new Set(expectedIps);
    for (const ip of visibleIps) {
      expect(expectedSet.has(ip)).toBe(true);
    }
  }

  async filterModalType(type: string) {
    await this.page.getByTestId("modal").getByTestId("filter-dropdown-Type").click();
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await expect(popover).toHaveCSS("opacity", "1");
    await this.clickDropdownFilterOption(popover, [type]);
    await popover.getByRole("button", { name: "Apply" }).click();
    await expect(popover).toBeHidden();
  }

  async filterModalGroup(groupName: string) {
    await this.page.getByTestId("modal").getByTestId("filter-dropdown-Group").click();
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await expect(popover).toHaveCSS("opacity", "1");

    const resetButton = popover.getByRole("button", { name: "Reset" });
    await resetButton.click();

    await popover.getByText(groupName, { exact: true }).click();
    await popover.getByRole("button", { name: "Apply" }).click();
    await expect(popover).toBeHidden();
  }

  async clickSaveInModal() {
    await this.clickIn("Save", "modal");
  }

  async clickDeleteGroupInModal() {
    await this.clickIn("Delete group", "modal");
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

  private getGroupRow(groupName: string) {
    return this.page
      .getByTestId("list-row")
      .filter({ has: this.page.getByTestId("name").getByText(groupName, { exact: true }) })
      .first();
  }
}
