import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { PROTO_RIG_DISPLAY_NAME } from "../helpers/minerModels";
import { BasePage } from "./base";

export class AddMinersPage extends BasePage {
  async clickFindMinersInNetwork() {
    await this.clickIn("Find miners", "section-scan-network");
  }

  async clickFindMinersByIp() {
    await this.clickIn("Find miners", "section-search-by-ip");
  }

  async inputMinerIp(ipAddresses: string) {
    await this.page.locator('//textarea[@id="ipAddresses"]').fill(ipAddresses);
  }

  async clickChooseMiners() {
    await this.clickButton("Choose miners");
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
    await this.page.getByRole("button", { name: `Continue with ${minerCount} miners` }).click();
  }

  async clickContinueWithSelectedMiners() {
    await this.page.getByRole("button", { name: /Continue with \d+ miner(s)?/ }).click();
  }

  async waitForFoundMinersList() {
    const foundMinersList = this.page.getByTestId("found-miners-list");
    await expect(foundMinersList).toBeVisible();
  }

  async waitForNetworkScanToFinish() {
    await this.waitForFoundMinersList();

    await expect(async () => {
      const scanningButton = this.page.getByRole("button", { name: "Scanning", exact: true });
      expect(await scanningButton.isVisible().catch(() => false)).toBe(false);
      await expect(this.page.getByRole("button", { name: "Rescan network", exact: true })).toBeVisible();
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async getSelectedMinersCount(): Promise<number> {
    const continueButton = this.page.getByRole("button", { name: /Continue with \d+ miner(s)?/ }).first();

    if (!(await continueButton.isVisible().catch(() => false))) {
      return 0;
    }

    const buttonText = (await continueButton.innerText()).trim();
    const match = buttonText.match(/Continue with (\d+) miner(?:s)?/);

    if (!match) {
      throw new Error(`Could not parse selected miner count from button text: "${buttonText}"`);
    }

    return Number.parseInt(match[1], 10);
  }

  async waitForExpectedNetworkMinerCount(expectedMinerCount: number, maxAttempts: number = 2) {
    for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
      await this.waitForNetworkScanToFinish();
      const foundMinerCount = await this.getSelectedMinersCount();

      if (foundMinerCount === expectedMinerCount) {
        return;
      }

      if (attempt === maxAttempts) {
        throw new Error(
          `Expected ${expectedMinerCount} miners after network scan, but found ${foundMinerCount} after ${maxAttempts} attempt(s).`,
        );
      }

      await this.page.getByRole("button", { name: "Rescan network", exact: true }).click();
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
