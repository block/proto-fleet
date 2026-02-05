import { expect, type Locator } from "@playwright/test";
import { BasePage } from "./base";

export class AddMinersPage extends BasePage {
  async clickFindMinersInNetwork() {
    await this.clickIn("Find miners", "section-scan-network");
  }

  async clickFindMinersByIp() {
    await this.clickIn("Find miners", "section-search-by-ip");
  }

  async inputMinerIp(ipAddresses: string) {
    await this.page.fill('//textarea[@id="ipAddresses"]', ipAddresses);
  }

  async clickChooseMiners() {
    await this.click("Choose miners");
  }

  async clickSelectNone() {
    await this.click("Select none");
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
    await this.click("Done");
  }

  async clickContinueWithXMiners(minerCount: number) {
    await this.page.getByRole("button", { name: `Continue with ${minerCount} miners` }).click();
  }

  async validateOneMinerWasFoundByIp() {
    const foundMessage = this.page.getByText("1 miners found on your network");
    await expect(foundMessage).toBeVisible();

    const minerRows = this.page.getByTestId("miner-model-row");
    await expect(minerRows).toHaveCount(1);

    const firstMinerRow = minerRows.first();
    await expect(firstMinerRow).toContainText("Proto Rig");
    await expect(firstMinerRow).toContainText("1 miners");

    const continueButton = this.page.getByRole("button", { name: "Continue with 1 miners" });
    await expect(continueButton).toBeVisible();
  }
}
