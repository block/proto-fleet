import { expect } from "@playwright/test";
import { BasePage } from "../base";

export class ParentPickerModalComponent extends BasePage {
  private modal() {
    return this.page.getByTestId("modal");
  }

  async validateTitleMatches(title: RegExp) {
    await expect(this.modal()).toContainText(title);
  }

  async selectOption(name: string) {
    const option = this.modal().locator("label").filter({ hasText: name }).first();
    await expect(option).toBeVisible();
    await option.click();
  }

  async clickSave() {
    await this.clickIn("Save", "modal");
  }

  async continueSiteMoveIfVisible() {
    const dialogTitle = this.page.getByText("Move miners between sites?", { exact: true });
    if (await dialogTitle.isVisible().catch(() => false)) {
      await this.page.getByRole("button", { name: "Continue", exact: true }).click();
    }
  }
}
