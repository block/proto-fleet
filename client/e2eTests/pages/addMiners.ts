import type { Locator } from "@playwright/test";
import { BasePage } from "./base";

export class AddMinersPage extends BasePage {
  async clickFindMiners() {
    await this.click("Find miners");
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
}
