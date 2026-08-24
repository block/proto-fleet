import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsUpdatesPage extends BasePage {
  async validateUpdatesPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/updates/);
    await this.validateTitle("Software Update");
  }

  async validateCurrentVersion(version: string) {
    await expect(this.page.getByTestId("current-version-row").getByText(version, { exact: true })).toBeVisible();
  }

  async validateLatestAvailableVersion(version: string) {
    await expect(this.page.getByTestId("available-update-lockup")).toContainText(`Fleet ${version} available`);
  }

  async validateReleaseNotesLink(url: string) {
    await expect(this.page.getByRole("link", { name: "Release notes", exact: true })).toHaveAttribute("href", url);
  }

  async validateInstallCommand(command: string) {
    await expect(this.page.getByTestId("manual-install-modal").locator("code")).toHaveText(command);
  }

  async validateCopyInstallCommandEnabled() {
    await expect(
      this.page.getByTestId("manual-install-modal").getByRole("button", { name: "Copy install command" }),
    ).toBeEnabled();
  }

  async clickInstallManually() {
    await this.page.getByRole("button", { name: "Install manually", exact: true }).click();
  }

  async closeManualInstall() {
    await this.page.getByTestId("manual-install-modal").getByRole("button", { name: "Close", exact: true }).click();
  }

  async clickIncludeReleaseCandidates() {
    await this.page.getByText("Include release candidates", { exact: true }).click();
  }

  async validateIncludeReleaseCandidatesChecked() {
    await expect(this.page.getByRole("checkbox", { name: "Include release candidates", exact: true })).toBeChecked();
  }

  async validateIncludeReleaseCandidatesUnchecked() {
    await expect(
      this.page.getByRole("checkbox", { name: "Include release candidates", exact: true }),
    ).not.toBeChecked();
  }

  async clickUpdateNow() {
    await this.page
      .getByTestId("available-update-lockup")
      .getByRole("button", { name: "Update now", exact: true })
      .click();
  }

  async validateUpgradeConfirmationOpened(version: string) {
    const modal = this.page.getByTestId("upgrade-operation-modal");
    await expect(modal).toBeVisible();
    await expect(modal).toContainText(`Update Fleet to ${version}`);
    await expect(modal).toContainText("Fleet will be unavailable for a few minutes while it updates.");
  }

  async confirmUpdate() {
    await this.page
      .getByTestId("upgrade-operation-modal")
      .getByRole("button", { name: "Update now", exact: true })
      .click();
  }

  async validateManualInstallActionHidden() {
    await expect(this.page.getByRole("button", { name: "Install manually", exact: true })).toHaveCount(0);
  }

  async validateUpgradeModalText(text: string) {
    await expect(this.page.getByTestId("upgrade-operation-modal")).toContainText(text);
  }
}
