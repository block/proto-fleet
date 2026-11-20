import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsPoolsPage extends BasePage {
  private row(name: string) {
    return this.page.getByRole("row").filter({ has: this.page.getByText(name, { exact: true }) });
  }

  async validateMiningPoolsSubmenuHidden() {
    await expect(this.page.getByTestId("secondary-nav").locator('a[href="/settings/mining-pools"]')).toBeHidden();
  }

  async validateMiningPoolsPageOpened() {
    await expect(this.page).toHaveURL(/\/mining-pools$/);
    await expect(this.page.getByText("Pool configurations", { exact: true })).toBeVisible();
  }

  async clickAddPool() {
    await this.page.getByRole("button", { name: "Create configuration", exact: true }).click();
  }

  async editConfiguration(name: string) {
    await this.row(name)
      .getByRole("button", { name: `Actions for ${name}`, exact: true })
      .click();
    await this.page.getByRole("button", { name: "Edit configuration", exact: true }).click();
  }

  async validatePoolEntryByUniqueName(name: string, url: string, username: string) {
    const row = this.row(name);
    await expect(row).toContainText(url);
    await row.click();
    const details = this.page.getByRole("dialog", { name: "Pool configuration", exact: true });
    await expect(details).toContainText(`Username: ${username}`);
    await this.page.keyboard.press("Escape");
    await expect(details).toBeHidden();
  }

  async deletePoolByNameIfVisible(name: string) {
    const row = this.row(name);
    if (!(await row.isVisible())) return;
    await row.getByRole("button", { name: `Actions for ${name}`, exact: true }).click();
    await this.page.getByRole("button", { name: "Delete configuration", exact: true }).click();
    const confirmation = this.page.getByRole("dialog", { name: "Delete configuration", exact: true });
    await confirmation.getByRole("button", { name: "Delete configuration", exact: true }).click();
    await expect(row).toHaveCount(0);
  }
}
