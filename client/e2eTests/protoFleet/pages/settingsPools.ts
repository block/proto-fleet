import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPoolsPage extends BasePage {
  async validateMiningPoolsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/mining-pools/);
    await this.validateButtonIsVisible("Add pool");
  }

  async clickAddPool() {
    await this.clickButton("Add pool");
  }

  async validatePoolEntryByUniqueName(expectedName: string, expectedUrl: string, expectedUsername: string) {
    await expect(this.page.getByTestId(`pool-row`).getByTestId("pool-name").getByText(expectedName)).toBeVisible();
    const row = this.page
      .getByTestId(`pool-row`)
      .filter({ has: this.page.getByTestId("pool-name").getByText(expectedName) });
    await expect(row.getByTestId("pool-url").getByText(expectedUrl)).toBeVisible();
    await expect(row.getByTestId("pool-username").getByText(expectedUsername)).toBeVisible();
  }

  async deleteAllPools() {
    const poolRows = this.page.getByTestId("pool-row");
    const poolCount = await poolRows.count();

    for (let i = 0; i < poolCount; i++) {
      const firstRow = poolRows.first();
      await firstRow.getByRole("button", { name: "Options menu", exact: true }).click();
      await this.clickButton("Delete pool");
      await expect(poolRows).toHaveCount(poolCount - 1 - i);
    }
    await expect(poolRows).toHaveCount(0);
  }
}
