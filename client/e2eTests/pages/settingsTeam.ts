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
    await this.click("Add team member");
  }

  async inputMemberUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  async clickSaveTeamMember() {
    await this.click("Save");
  }

  async validateMemberAdded() {
    await expect(this.page.locator(`//*[@data-testid="modal"]`).getByText("Member added")).toBeVisible();
  }

  async validateCopyPasswordButtonVisible() {
    await expect(this.page.locator(`//button[@aria-label="Copy password"]`)).toBeVisible();
  }

  async clickDone() {
    await this.click("Done");
  }

  async validateMemberRole(username: string, role: string) {
    const memberRow = this.page.locator(`//*[@data-testid='list-body']/tr`).filter({
      has: this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
    });
    await expect(memberRow.locator(`//td[@data-testid='role']`)).toHaveText(role);
  }

  async validateMemberLastLogin(username: string, lastLogin: string) {
    const memberRow = this.page.locator(`//*[@data-testid='list-body']/tr`).filter({
      has: this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
    });
    await expect(memberRow.locator(`//td[@data-testid='lastLoginAt']`)).toHaveText(lastLogin);
  }

  async getTemporaryPassword(): Promise<string> {
    return await this.page.locator(`//*[@data-testid="temporary-password"]`).innerText();
  }

  async validateMemberVisible(username: string) {
    await expect(this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`)).toBeVisible();
  }

  async validateNoAdminRights() {
    await expect(this.page.getByRole("button", { name: "Add team member" })).toBeHidden();
  }

  async clickMemberActionsMenu(username: string) {
    const memberRow = this.page.locator(`//*[@data-testid='list-body']/tr`).filter({
      has: this.page.locator(`//td[@data-testid='username']//*[text()='${username}']`),
    });
    await memberRow.locator(`//*[@data-testid="list-actions-trigger"]`).click();
  }

  async clickResetPassword() {
    await this.click("Reset Password");
  }

  async clickResetMemberPasswordConfirm() {
    await this.click("Reset member password");
  }

  async validatePasswordReset() {
    await expect(this.page.locator(`//*[@data-testid="modal"]`).getByText("Password reset")).toBeVisible();
  }

  async clickDeactivate() {
    await this.click("Deactivate");
  }

  async clickConfirmDeactivation() {
    await this.click("Confirm deactivation");
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
