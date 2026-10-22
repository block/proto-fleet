import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { PROTO_RIG_DISPLAY_NAME } from "../helpers/minerModels";
import { BasePage } from "./base";

const SHORT_CLOSE_TIMEOUT = Math.floor(DEFAULT_TIMEOUT / 6);
const NETWORK_SCAN_SUMMARY =
  /^(?:(?:\d+ containers?(?: and \d+ miners?)?|\d+ miners?) found on your network|No devices found)$/;

export class AddMinersPage extends BasePage {
  getRemoteNetworkPrerequisite(): Locator {
    return this.page.getByTestId("remote-network-prerequisite");
  }

  async openOnboarding() {
    await this.page.goto("/onboarding/miners");
  }

  getGetStartedButton() {
    return this.page.getByRole("button", { name: "Get started", exact: true });
  }

  async startOnboarding() {
    await this.getGetStartedButton().click();
  }

  async openFleetNodeSettings() {
    await this.getRemoteNetworkPrerequisite().getByRole("link", { name: "Manage Nodes" }).click();
  }

  getScanNetworkSection(): Locator {
    return this.page.getByTestId("section-scan-network");
  }

  getSearchByIpSection(): Locator {
    return this.page.getByTestId("section-search-by-ip");
  }

  getFoundMinersList(): Locator {
    return this.page.getByTestId("found-miners-list");
  }

  getValidationErrorDialog(): Locator {
    return this.page.getByTestId("validation-error-dialog");
  }

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
    await expect(this.page.getByTestId("section-scan-network")).toBeVisible();
    await expect(this.page.getByTestId("section-search-by-ip")).toBeVisible();
  }

  async validateAddMinersFlowClosed(timeout: number = SHORT_CLOSE_TIMEOUT) {
    await expect(this.page.getByLabel("Close add miners")).toBeHidden({ timeout });
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

  async selectOnlyMiners(expectedMinerCount: number) {
    const summary = await this.page.getByText(NETWORK_SCAN_SUMMARY).innerText();
    if (!/\d+ containers?/.test(summary)) {
      // A miner-only scan already selects exactly the desired device type.
      await expect(this.continueWithMinersButton(expectedMinerCount)).toBeVisible();
      return;
    }
    await this.clickChooseMiners();
    const modal = this.page.getByTestId("modal");
    const miners = modal.getByRole("region", { name: "Miners", exact: true });
    await expect(miners.getByTestId("list-body").locator("tr")).toHaveCount(expectedMinerCount);
    await this.clickSelectNone();
    await miners.getByTestId("select-all-checkbox").click();
    await expect(miners.getByTestId("list-body").locator('input[type="checkbox"]:checked')).toHaveCount(
      expectedMinerCount,
    );
    await expect(
      modal.getByRole("region", { name: "Containers", exact: true }).locator('input[type="checkbox"]:checked'),
    ).toHaveCount(0);
    await this.clickDone();
    await expect(this.continueWithMinersButton(expectedMinerCount)).toBeVisible();
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

  async waitForNetworkScanToFinish(timeout = DEFAULT_TIMEOUT) {
    await expect(async () => {
      const scanningButton = this.page.getByRole("button", { name: "Scanning", exact: true });
      const rescanNetworkButton = this.page.getByRole("button", { name: "Rescan network", exact: true });
      const foundMinersHeading = this.page.getByText(NETWORK_SCAN_SUMMARY);
      const foundMinersList = this.getFoundMinersList();
      const continueButton = this.continueWithMinersButton();

      expect(await scanningButton.isVisible().catch(() => false)).toBe(false);
      expect(await foundMinersHeading.isVisible().catch(() => false)).toBe(true);
      const noDevices = await this.page.getByText("No devices found", { exact: true }).isVisible();
      expect(await continueButton.isVisible().catch(() => false)).toBe(!noDevices);
      if (!noDevices) {
        expect(await foundMinersList.isVisible().catch(() => false)).toBe(true);
      }

      if (!this.isMobile) {
        expect(await rescanNetworkButton.isVisible().catch(() => false)).toBe(true);
      }
    }).toPass({ timeout, intervals: [DEFAULT_INTERVAL] });
  }

  async waitForNetworkScanToStart() {
    if (this.isMobile) {
      await expect(this.page.getByText(/^(Finding devices on your network|Scanning…)/)).toBeVisible({
        timeout: DEFAULT_TIMEOUT,
      });
      return;
    }

    await expect(async () => {
      const scanningButton = this.page.getByRole("button", { name: "Scanning", exact: true });
      expect(await scanningButton.isVisible().catch(() => false)).toBe(true);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async waitForExpectedNetworkMinerCount(expectedMinerCount: number, maxAttempts: number = 2) {
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      if (attempt > 1) {
        await this.waitForNetworkScanToStart();
      }

      await this.waitForNetworkScanToFinish();
      // The continue button counts all selected devices, including containers.
      // Read the miner bucket in the scan summary instead.
      const minerCount = await this.getFoundMinersCount();

      if (minerCount === expectedMinerCount) {
        return;
      }

      if (attempt === maxAttempts) {
        throw new Error(
          `Expected ${expectedMinerCount} discovered miners after network scan, but got ${minerCount} after ${maxAttempts} attempt(s).`,
        );
      }

      await this.clickRescanNetwork();
    }
  }

  async getFoundMinersCount(): Promise<number> {
    const summary = await this.page.getByText(NETWORK_SCAN_SUMMARY).innerText();
    const match = summary.match(/(\d+) miners? found on your network/);
    return match ? Number.parseInt(match[1], 10) : 0;
  }

  async clickHeaderIconButton() {
    await this.page.getByTestId("header-icon-button").click();
    await this.validateAddMinersFlowClosed();
  }

  async closeAddMinersFlowIfOpen(timeout: number = SHORT_CLOSE_TIMEOUT) {
    const closeButton = this.page.getByLabel("Close add miners");
    if (!(await closeButton.isVisible().catch(() => false))) {
      return;
    }

    await closeButton.click();
    await this.validateAddMinersFlowClosed(timeout);
  }

  async validateOneMinerWasFoundByIp() {
    const foundMessage = this.page.getByText("1 miner found on your network");
    await expect(foundMessage).toBeVisible();

    const minerRows = this.page.getByTestId("miner-model-row");
    await expect(minerRows).toHaveCount(1);

    const firstMinerRow = minerRows.first();
    await expect(firstMinerRow).toContainText(PROTO_RIG_DISPLAY_NAME);
    await expect(firstMinerRow).toContainText("1 miner");

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
