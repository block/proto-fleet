import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { PROTO_RIG_DISPLAY_NAME } from "../helpers/minerModels";
import { BasePage } from "./base";

export class AddMinersPage extends BasePage {
  private continueWithMinersButton(minerCount?: number) {
    return minerCount === undefined
      ? this.page.getByRole("button", { name: /Continue with \d+ miner(s)?/ }).first()
      : this.page.getByRole("button", { name: `Continue with ${minerCount} miners`, exact: true });
  }

  private async clickRescanNetwork() {
    if (!this.isMobile) {
      await this.page.getByTestId("add-miners-rescan-network").click();
      return;
    }

    await this.page.getByTestId("add-miners-more-actions").click();
    await this.page.getByTestId("add-miners-rescan-network-overflow-item").click();
  }

  async clickFindMinersInNetwork() {
    await this.clickIn("Find miners", "section-scan-network");
  }

  async clickFindMinersByIp() {
    await this.clickIn("Find miners", "section-search-by-ip");
  }

  async validateAddMinersFlowOpened() {
    await expect(this.page.getByText("Add miners", { exact: true })).toBeVisible();
    await expect(this.page.getByTestId("section-scan-network")).toBeVisible();
    await expect(this.page.getByTestId("section-search-by-ip")).toBeVisible();
  }

  async inputMinerIp(ipAddresses: string) {
    await this.page.locator('//textarea[@id="ipAddresses"]').fill(ipAddresses);
  }

  async clickChooseMiners() {
    if (!this.isMobile) {
      await this.page.getByTestId("add-miners-choose-miners").click();
      return;
    }

    await this.page.getByTestId("add-miners-more-actions").click();
    await this.page.getByTestId("add-miners-choose-miners-overflow-item").click();
  }

  async clickSelectAllCheckboxInModal() {
    await this.page.getByTestId("modal").getByTestId("select-all-checkbox").click();
  }

  async clickSelectNone() {
    await this.clickButton("Select none");
  }

  async getMinerIpAddressByIndex(index: number): Promise<string> {
    const rows = this.page.getByTestId("modal").getByTestId("list-body").locator("tr");
    const row = rows.nth(index);
    return await row.getByTestId("ipAddress").innerText();
  }

  async getMinerRowByIp(ipAddress: string): Promise<Locator> {
    return this.page
      .getByTestId("modal")
      .locator(`//tr[child::*[@data-testid="ipAddress" and descendant::text()='${ipAddress}']]`);
  }

  async clickMinerCheckbox(ipAddress: string) {
    const minerRow = await this.getMinerRowByIp(ipAddress);
    await minerRow.locator('input[type="checkbox"]').click();
  }

  async clickDone() {
    await this.clickButton("Done");
  }

  async clickContinueWithXMiners(minerCount: number) {
    await this.continueWithMinersButton(minerCount).click();
  }

  async clickContinueWithSelectedMiners() {
    await this.continueWithMinersButton().click();
  }

  async waitForFoundMinersList() {
    const foundMinersList = this.page.getByTestId("found-miners-list");
    await expect(foundMinersList).toBeVisible();
  }

  async waitForNetworkScanToFinish() {
    await this.waitForFoundMinersList();

    await expect(async () => {
      const scanningButton = this.page.getByRole("button", { name: "Scanning", exact: true });
      const rescanNetworkButton = this.page.getByRole("button", { name: "Rescan network", exact: true });
      const foundMinersHeading = this.page.getByText(/\d+ miners found on your network/);
      const continueButton = this.continueWithMinersButton();

      expect(await scanningButton.isVisible().catch(() => false)).toBe(false);
      expect(await foundMinersHeading.isVisible().catch(() => false)).toBe(true);
      expect(await continueButton.isVisible().catch(() => false)).toBe(true);

      if (!this.isMobile) {
        expect(await rescanNetworkButton.isVisible().catch(() => false)).toBe(true);
      }
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async waitForNetworkScanToStart() {
    if (this.isMobile) {
      await expect(this.page.getByText(/^Finding miners on your network/)).toBeVisible({ timeout: DEFAULT_TIMEOUT });
      return;
    }

    await expect(async () => {
      const scanningButton = this.page.getByRole("button", { name: "Scanning", exact: true });
      expect(await scanningButton.isVisible().catch(() => false)).toBe(true);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async getSelectedMinersCount(): Promise<number> {
    const continueButton = this.page.getByRole("button", { name: /Continue with \d+ miner(s)?/ }).first();

    if (!(await continueButton.isVisible().catch(() => false))) {
      return 0;
    }

    const buttonText = ((await continueButton.getAttribute("aria-label")) ?? (await continueButton.innerText())).trim();
    const match = buttonText.match(/Continue with (\d+) miner(?:s)?/);

    if (!match) {
      throw new Error(`Could not parse selected miner count from button text: "${buttonText}"`);
    }

    return Number.parseInt(match[1], 10);
  }

  async waitForExpectedNetworkMinerCount(expectedMinerCount: number, maxAttempts: number = 2) {
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      if (attempt > 1) {
        await this.waitForNetworkScanToStart();
      }

      await this.waitForNetworkScanToFinish();
      const selectedMinerCount = await this.getSelectedMinersCount();

      if (selectedMinerCount === expectedMinerCount) {
        return;
      }

      if (attempt === maxAttempts) {
        throw new Error(
          `Expected ${expectedMinerCount} selected miners after network scan, but got ${selectedMinerCount} after ${maxAttempts} attempt(s).`,
        );
      }

      await this.clickRescanNetwork();
    }
  }

  async getFoundMinersCount(): Promise<number> {
    const minerRows = this.page.getByTestId("miner-model-row");
    return await minerRows.count();
  }

  async clickHeaderIconButton() {
    await this.page.getByTestId("header-icon-button").click();
  }

  async validateOneMinerWasFoundByIp() {
    const foundMessage = this.page.getByText("1 miners found on your network");
    await expect(foundMessage).toBeVisible();

    const minerRows = this.page.getByTestId("miner-model-row");
    await expect(minerRows).toHaveCount(1);

    const firstMinerRow = minerRows.first();
    await expect(firstMinerRow).toContainText(PROTO_RIG_DISPLAY_NAME);
    await expect(firstMinerRow).toContainText("1 miners");

    const continueButton = this.page.getByRole("button", { name: "Continue with 1 miners" });
    await expect(continueButton).toBeVisible();
  }

  async validateValidationErrorDialogIsVisible() {
    const dialog = this.page.getByTestId("validation-error-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("Some entries not recognized")).toBeVisible();
  }

  async validateValidationErrorDialogIsClosed() {
    await expect(this.page.getByTestId("validation-error-dialog")).toBeHidden();
  }

  async validateInvalidIpAddressesInDialog(entries: string[]) {
    const dialog = this.page.getByTestId("validation-error-dialog");
    await expect(dialog.getByText("Invalid IP addresses")).toBeVisible();
    for (const entry of entries) {
      await expect(dialog.getByText(entry)).toBeVisible();
    }
  }

  async validateInvalidIpRangesInDialog(entries: string[]) {
    const dialog = this.page.getByTestId("validation-error-dialog");
    await expect(dialog.getByText("Invalid IP ranges")).toBeVisible();
    for (const entry of entries) {
      await expect(dialog.getByText(entry)).toBeVisible();
    }
  }

  async validateInvalidSubnetsInDialog(entries: string[]) {
    const dialog = this.page.getByTestId("validation-error-dialog");
    await expect(dialog.getByText("Invalid subnet blocks")).toBeVisible();
    for (const entry of entries) {
      await expect(dialog.getByText(entry)).toBeVisible();
    }
  }

  async clickBackToEditing() {
    await this.page.getByTestId("validation-error-dialog").getByRole("button", { name: "Back to editing" }).click();
  }

  async clickContinueAnyway() {
    await this.page.getByTestId("validation-error-dialog").getByRole("button", { name: "Continue anyway" }).click();
  }

  async validateContinueAnywayButtonNotVisible() {
    const dialog = this.page.getByTestId("validation-error-dialog");
    await expect(dialog.getByRole("button", { name: "Continue anyway" })).toBeHidden();
  }

  async validateContinueAnywayButtonVisible() {
    const dialog = this.page.getByTestId("validation-error-dialog");
    await expect(dialog.getByRole("button", { name: "Continue anyway" })).toBeVisible();
  }

  async validateTextareaErrorContains(text: string) {
    const errorElement = this.page.getByTestId("ipAddresses-validation-error");
    await expect(errorElement).toContainText(text);
  }
}
