import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPoolsPage extends BasePage {
  private getPoolRowByName(name: string) {
    return this.page
      .getByTestId("pool-row")
      .filter({ has: this.page.getByTestId("pool-name").getByText(name, { exact: true }) })
      .first();
  }

  async validateMiningPoolsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/mining-pools/);
    await this.validateButtonIsVisible("Add pool");
  }

  async clickAddPool() {
    await this.clickButton("Add pool");
  }

  async validatePoolEntryByUniqueName(expectedName: string, expectedUrl: string, expectedUsername: string) {
    await expect(this.page.getByTestId(`pool-row`).getByTestId("pool-name").getByText(expectedName)).toBeVisible();
    const row = this.getPoolRowByName(expectedName);
    await expect(row.getByTestId("pool-url").getByText(expectedUrl)).toBeVisible();
    await expect(row.getByTestId("pool-username").getByText(expectedUsername)).toBeVisible();
  }

  async deletePoolByNameIfVisible(name: string) {
    const row = this.getPoolRowByName(name);
    if (!(await row.isVisible().catch(() => false))) {
      return;
    }

    await row.getByRole("button", { name: "Options menu", exact: true }).click();
    await this.clickButton("Delete pool");
    await expect(row).toHaveCount(0);
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
