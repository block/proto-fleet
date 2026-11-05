import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class EditPoolPage extends BasePage {
  private picker() {
    return this.page.getByRole("dialog", { name: "Edit pool", exact: true });
  }

  async openAccountSelection(accountId: string, count: number) {
    if (count === 1) await this.page.goto(`/accounts/${accountId}/assets/miner/42`);
    else {
      await this.page.goto(`/accounts/${accountId}/assets`);
      await this.page.getByRole("checkbox", { name: "Select Miner 42", exact: true }).check();
      await this.page.getByRole("checkbox", { name: "Select Miner 43", exact: true }).check();
    }
    await this.page.getByRole("button", { name: "Edit pool", exact: true }).click();
    await this.page.getByLabel("Username", { exact: true }).fill("customer-admin");
    await this.page.getByLabel("Password", { exact: true }).fill("first-password");
    await this.page.getByRole("button", { name: "Continue", exact: true }).click();
  }

  async assignExpectingUnknown(count: number) {
    if (count > 1) {
      await this.reviewAssignment(count);
      await this.page
        .getByRole("dialog", { name: /Assign pool configuration to/ })
        .getByRole("button", { name: "Assign", exact: true })
        .click();
    } else await this.picker().getByRole("button", { name: "Assign", exact: true }).click();
    const unknown = this.page.getByRole("dialog", { name: /Assign pool configuration to/ });
    await expect(unknown).toContainText("It may still be running.");
    await unknown.getByRole("button", { name: "Close", exact: true }).click();
  }

  async createConfiguration() {
    await this.picker().getByRole("button", { name: "New pool configuration", exact: true }).click();
  }

  async selectConfiguration(name: string) {
    await this.picker().getByRole("radio", { name, exact: true }).check();
  }

  async validateSelectedConfiguration(name: string) {
    await expect(this.picker().getByRole("radio", { name, exact: true })).toBeChecked();
  }

  async selectedConfigurationName() {
    const label = this.picker()
      .locator("label")
      .filter({ has: this.page.getByRole("radio", { checked: true }) });
    await expect(label).toHaveCount(1);
    return (await label.innerText()).trim();
  }

  async validatePoolVisible(name: string, url: string) {
    const row = this.picker()
      .getByRole("row")
      .filter({ has: this.page.getByRole("radio", { name, exact: true }) });
    await expect(row).toContainText(url);
  }

  async assignSingle() {
    await this.picker().getByRole("button", { name: "Assign", exact: true }).click();
    await expect(this.picker()).toBeHidden();
    await expect(this.page.getByRole("dialog", { name: /Assign pool configuration to/ })).toHaveCount(0);
  }

  async reviewAssignment(count: number) {
    await this.picker().getByRole("button", { name: "Assign", exact: true }).click();
    const review = this.page.getByRole("dialog", { name: /Assign pool configuration to/ });
    await expect(review).toHaveAccessibleName(`Assign pool configuration to ${count} miner${count === 1 ? "" : "s"}?`);
    await expect(review.getByRole("region", { name: "Affected equipment" })).toHaveCount(0);
  }

  async restoreConfiguration(name: string, overviewUrl: string, count: number, openPicker: () => Promise<void>) {
    if (!name || this.page.isClosed()) return;
    await this.page.goto(overviewUrl);
    await openPicker();
    await this.selectConfiguration(name);
    await this.reviewAssignment(count);
    await this.confirmAssignment();
  }

  async confirmAssignment() {
    const review = this.page.getByRole("dialog", { name: /Assign pool configuration to/ });
    await review.getByRole("button", { name: "Assign", exact: true }).click();
    await expect(review).toBeHidden();
  }
}
