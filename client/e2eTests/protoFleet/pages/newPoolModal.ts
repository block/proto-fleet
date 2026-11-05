import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class NewPoolModalPage extends BasePage {
  private editor() {
    return this.page.getByRole("dialog", { name: /^(Create|Edit) pool configuration$/ });
  }

  async validatePoolModalOpened() {
    await expect(this.editor().getByRole("group", { name: "Primary pool", exact: true })).toBeVisible();
  }

  async inputPoolName(name: string) {
    await this.editor().getByLabel("Name", { exact: true }).fill(name);
  }

  async inputPoolUrl(url: string) {
    await this.editor()
      .getByRole("group", { name: "Primary pool", exact: true })
      .getByLabel("Pool URL", { exact: true })
      .fill(url);
  }

  async inputPoolUsername(username: string) {
    await this.editor()
      .getByRole("group", { name: "Primary pool", exact: true })
      .getByLabel("Username", { exact: true })
      .fill(username);
  }

  async validateConnectionDisabled() {
    await expect(
      this.editor()
        .getByRole("group", { name: "Primary pool", exact: true })
        .getByRole("button", { name: "Test connection", exact: true }),
    ).toBeDisabled();
  }

  async clickTestConnection() {
    await this.editor()
      .getByRole("group", { name: "Primary pool", exact: true })
      .getByRole("button", { name: "Test connection", exact: true })
      .click();
  }

  async validateConnectionFailed() {
    await expect(this.editor().getByRole("status")).toContainText("We couldn't connect with your pool.");
  }

  async validateConnectionSuccessful() {
    await expect(this.editor().getByRole("status")).toContainText("Pool connection successful");
  }

  async addBackup(url: string, username: string) {
    await this.editor().getByRole("button", { name: "Add backup pool", exact: true }).click();
    const backup = this.editor().getByRole("group", { name: "Backup 1", exact: true });
    await backup.getByLabel("Pool URL", { exact: true }).fill(url);
    await backup.getByLabel("Username", { exact: true }).fill(username);
  }

  async moveBackupToPrimary() {
    await this.editor()
      .getByRole("group", { name: "Backup 1", exact: true })
      .getByRole("button", { name: "Move up", exact: true })
      .click();
  }

  async clickSaveNewPool() {
    await this.editor().getByRole("button", { name: "Save configuration", exact: true }).click();
    await expect(this.editor()).toBeHidden();
  }
}
