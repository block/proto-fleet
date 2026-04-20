import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsTeamPage extends BasePage {
  async validateTeamSettingsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/team/);
    await this.validateTitle("Team");
  }

  async validateIsAdmin() {
    await expect(this.page.getByRole("button", { name: "Add team member" })).toBeVisible();
  }

  async clickAddTeamMember() {
    await this.clickButton("Add team member");
  }

  async inputMemberUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  async clickSaveTeamMember() {
    await this.clickButton("Save");
  }

  async validateMemberAdded() {
    await expect(this.page.getByTestId("modal").getByText("Member added")).toBeVisible();
  }

  async validateCopyPasswordButtonVisible() {
    await expect(this.page.locator(`//button[@aria-label="Copy password"]`)).toBeVisible();
  }

  async clickDone() {
    await this.clickButton("Done");
  }

  async validateMemberRole(username: string, role: string) {
    const memberRow = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({
        has: this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
      });
    await expect(memberRow.locator(`//td[@data-testid='role']`)).toHaveText(role);
  }

  async validateMemberLastLogin(username: string, lastLogin: string) {
    const memberRow = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({
        has: this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
      });
    await expect(memberRow.locator(`//td[@data-testid='lastLoginAt']`)).toHaveText(lastLogin);
  }

  async getTemporaryPassword(): Promise<string> {
    return await this.page.getByTestId("temporary-password").innerText();
  }

  async validateMemberVisible(username: string) {
    await expect(this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`)).toBeVisible();
  }

  async validateNoAdminRights() {
    await expect(this.page.getByRole("button", { name: "Add team member" })).toBeHidden();
  }

  async clickMemberActionsMenu(username: string) {
    const memberRow = this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({
        has: this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
      });
    await memberRow.getByTestId("list-actions-trigger").click();
  }

  async clickResetPassword() {
    await this.clickButton("Reset Password");
  }

  async clickResetMemberPasswordConfirm() {
    await this.clickButton("Reset member password");
  }

  async validatePasswordReset() {
    await expect(this.page.getByTestId("temporary-password")).toBeVisible();
    await expect(this.page.getByRole("button", { name: "Done", exact: true })).toBeVisible();
  }

  async clickDeactivate() {
    await this.clickButton("Deactivate");
  }

  async clickConfirmDeactivation() {
    await this.clickButton("Confirm deactivation");
  }

  async validateMemberDeactivatedMessage(username: string) {
    await expect(
      this.page.locator(`//*[contains(@class,'heading')][contains(text(),'${username} has been deactivated')]`),
    ).toBeVisible();
  }

  async validateMemberNotInList(username: string) {
    await expect(this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`)).toBeHidden();
  }
}
