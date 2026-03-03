import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPoolsPage extends BasePage {
  async validateMiningPoolsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/mining-pools/);
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
}
