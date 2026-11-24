import { expect, type Page } from "@playwright/test";

export class ActivationPoolRecoveryPage {
  constructor(private readonly page: Page) {}

  private async authenticate() {
    await this.page.getByLabel("Username", { exact: true }).fill("host-operator");
    await this.page.getByLabel("Password", { exact: true }).fill("test-password");
    await this.page.getByRole("button", { name: "Continue", exact: true }).click();
  }

  async removeContainerAssignment() {
    await this.page.goto("/containers/ct1");
    await this.page.getByRole("button", { name: "Actions for Recovery CT1", exact: true }).click();
    await this.page.getByRole("button", { name: "Container pool assignment", exact: true }).click();
    await this.authenticate();
    const removal = this.page.getByRole("dialog", { name: "Remove pool configuration?" });
    await expect(removal.getByRole("button", { name: "Clear pools and remove assignment" })).toHaveCount(0);
    await expect(removal.getByRole("checkbox", { name: "Keep the device’s current pool settings" })).toHaveCount(0);
    await expect(removal).toContainText("Its current pool settings and mining state will remain unchanged.");
    await removal.getByRole("button", { name: "Remove assignment only" }).click();
  }

  async assignModule() {
    await this.page.goto("/containers/ct1/tanks/81/modules/91");
    await this.page.getByRole("button", { name: "Actions for Module 1", exact: true }).click();
    await this.page.getByRole("button", { name: "Edit pool", exact: true }).click();
    await this.authenticate();
    const picker = this.page.getByRole("dialog", { name: "Edit pool", exact: true });
    await picker.getByRole("radio", { name: "Production", exact: true }).check();
    await expect(picker).toContainText("1 hashboards selected");
    await picker.getByRole("button", { name: "Assign", exact: true }).click();
    await expect(picker).toBeHidden();
  }
}
