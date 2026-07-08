import { expect } from "@playwright/test";
import { BasePage } from "./base";

const escapeRegex = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

export class SettingsTeamPage extends BasePage {
  private memberRow(username: string) {
    return this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({
        has: this.page.getByTestId("username").getByText(username, { exact: true }),
      });
  }

  private roleRow(roleName: string) {
    return this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({
        has: this.page.getByTestId("name").getByText(roleName, { exact: true }),
      });
  }

  private sanitizePermissionKey(permissionKey: string) {
    return permissionKey.replace(/[^a-z0-9]+/gi, "-").toLowerCase();
  }

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

  async openMembersTab() {
    const activateButton = this.page.getByTestId("team-tab-members-activate");
    if (await activateButton.isVisible().catch(() => false)) {
      await activateButton.click();
    }

    await expect(this.page.getByRole("button", { name: "Add team member", exact: true })).toBeVisible();
  }

  async openRolesTab() {
    const activateButton = this.page.getByTestId("team-tab-roles-activate");
    if (await activateButton.isVisible().catch(() => false)) {
      await activateButton.click();
    }

    await expect(this.page.getByRole("button", { name: "Create role", exact: true })).toBeVisible();
  }

  async inputMemberUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  private async pickRoleFromOpenModal(roleLabel: string) {
    await this.page.getByTestId("modal").getByRole("button", { name: "Role" }).click();
    // The Select option's accessible name is "<label> <description>"
    // (e.g. "Field Tech Field Tech role"), so we can't use `exact: true`
    // — match the role label as the visible text inside the option's
    // label slot, which uniquely identifies the row.
    await this.page.getByRole("option").filter({ hasText: roleLabel }).click();
  }

  async selectMemberRole(roleLabel: string) {
    await this.pickRoleFromOpenModal(roleLabel);
  }

  async createTeamMemberAndGetTemporaryPassword(username: string, roleLabel: string) {
    await this.clickAddTeamMember();
    await this.inputMemberUsername(username);
    await this.selectMemberRole(roleLabel);
    await this.clickSaveTeamMember();
    await this.validateMemberAdded();

    const temporaryPassword = await this.getTemporaryPassword();
    await this.clickDone();
    await this.validateMemberVisible(username);
    return temporaryPassword;
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
    await expect(this.memberRow(username).getByTestId("role")).toHaveText(role);
  }

  async validateMemberLastLogin(username: string, lastLogin: string) {
    await expect(this.memberRow(username).getByTestId("lastLoginAt")).toHaveText(lastLogin);
  }

  async getTemporaryPassword(): Promise<string> {
    return await this.page.getByTestId("temporary-password").innerText();
  }

  async validateMemberVisible(username: string) {
    await expect(this.memberRow(username)).toBeVisible();
  }

  // FIELD_TECH (and any role without user:read) doesn't see the Team
  // submenu in the Settings secondary nav at all — verifying the link's
  // absence is a stronger and cheaper "no admin rights" check than
  // landing on /team and asserting the Add button hidden.
  async validateTeamSubmenuHidden() {
    await expect(this.page.getByTestId("secondary-nav").locator('a[href="/settings/team"]')).toBeHidden();
  }

  async clickCreateRole() {
    await this.clickButton("Create role");
  }

  async inputRoleName(roleName: string) {
    await this.page.locator("#role-name").fill(roleName);
  }

  async inputRoleDescription(description: string) {
    await this.page.locator("#role-description").fill(description);
  }

  async selectRolePermission(permissionKey: string) {
    await this.page.getByTestId("role-permission-search").fill(permissionKey);

    const permissionRow = this.page.getByTestId(`role-permission-${this.sanitizePermissionKey(permissionKey)}`);
    await expect(permissionRow).toBeVisible();

    const checkbox = permissionRow.getByRole("checkbox");
    if (!(await checkbox.isChecked())) {
      await permissionRow.click();
    }
  }

  async clickCreateRoleConfirm() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Create role", exact: true }).click();
  }

  async createCustomRole(roleName: string, description: string, permissionKeys: string[]) {
    await this.clickCreateRole();
    await this.inputRoleName(roleName);
    await this.inputRoleDescription(description);

    for (const permissionKey of permissionKeys) {
      await this.selectRolePermission(permissionKey);
    }

    await this.page.getByTestId("role-permission-search").clear();
    await this.clickCreateRoleConfirm();
    await expect(this.page.getByTestId("modal")).toBeHidden();
    await this.validateRoleVisible(roleName);
  }

  async validateRoleVisible(roleName: string) {
    await expect(this.roleRow(roleName)).toBeVisible();
  }

  async deactivateMembersByPrefix(usernamePrefix: string) {
    await this.openMembersTab();
    const memberRows = await this.page.getByTestId("list-row").all();
    const usernamesToDeactivate: string[] = [];

    for (const row of memberRows) {
      const usernameElement = row.getByTestId("username").locator("span");
      const username = (await usernameElement.textContent())?.trim();
      if (username?.startsWith(usernamePrefix)) {
        usernamesToDeactivate.push(username);
      }
    }

    for (const username of usernamesToDeactivate) {
      await this.clickMemberActionsMenu(username);
      await this.clickDeactivate();
      await this.clickConfirmDeactivation();
      await this.validateMemberNotInList(username);
    }
  }

  async deleteRolesByPrefix(rolePrefix: string) {
    await this.openRolesTab();
    const roleRows = await this.page.getByTestId("list-row").all();
    const roleNamesToDelete: string[] = [];

    for (const row of roleRows) {
      const roleName = (
        await row
          .getByTestId("name")
          .getByText(new RegExp(`^${escapeRegex(rolePrefix)}`))
          .textContent()
          .catch(() => null)
      )?.trim();
      if (roleName?.startsWith(rolePrefix)) {
        roleNamesToDelete.push(roleName);
      }
    }

    for (const roleName of roleNamesToDelete) {
      const row = this.roleRow(roleName);
      await expect(row).toBeVisible();
      await row.getByTestId("list-actions-trigger").click();
      await this.clickButton("Delete");
      await this.clickButton("Delete role");
      await expect(this.roleRow(roleName)).toBeHidden();
    }
  }

  async clickMemberActionsMenu(username: string) {
    await this.memberRow(username).getByTestId("list-actions-trigger").click();
  }

  async clickResetPassword() {
    await this.clickButton("Reset Password");
  }

  async clickEditRole() {
    await this.clickButton("Edit role");
  }

  async selectEditedRole(roleLabel: string) {
    await this.pickRoleFromOpenModal(roleLabel);
  }

  async clickSaveEditedRole() {
    await this.clickButton("Save");
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
    await expect(this.memberRow(username)).toBeHidden();
  }
}
