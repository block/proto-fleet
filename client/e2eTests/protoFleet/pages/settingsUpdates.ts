import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class SettingsUpdatesPage extends BasePage {
  async validateUpdatesPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/updates/);
    await this.validateTitle("Updates");
  }

  async validateCurrentVersion(version: string) {
    await expect(this.rowByLabel("Current version").locator("> div").last()).toHaveText(version);
  }

  async validateLatestAvailableVersion(version: string) {
    await expect(this.rowByLabel("Latest available").locator("span").first()).toHaveText(version);
  }

  async validateReleaseNotesLink(url: string) {
    await expect(this.page.getByRole("link", { name: "Release notes", exact: true })).toHaveAttribute("href", url);
  }

  async validateInstallCommand(command: string) {
    await expect(this.page.locator("code").filter({ hasText: command })).toBeVisible();
  }

  async validateCopyInstallCommandEnabled() {
    await expect(this.page.getByRole("button", { name: "Copy install command", exact: true })).toBeEnabled();
  }

  async validateCopyInstallCommandDisabled() {
    await expect(this.page.getByRole("button", { name: "Copy install command", exact: true })).toBeDisabled();
  }

  async clickIncludeReleaseCandidates() {
    await this.page.getByRole("checkbox", { name: "Include release candidates", exact: true }).click();
  }

  async validateIncludeReleaseCandidatesChecked() {
    await expect(this.page.getByRole("checkbox", { name: "Include release candidates", exact: true })).toBeChecked();
  }

  async validateIncludeReleaseCandidatesUnchecked() {
    await expect(
      this.page.getByRole("checkbox", { name: "Include release candidates", exact: true }),
    ).not.toBeChecked();
  }

  async clickUpgradeToVersion(version: string) {
    await this.page.getByRole("button", { name: `Upgrade to ${version}`, exact: true }).click();
  }

  async validateUpgradeConfirmationOpened(version: string) {
    const modal = this.page.getByTestId("upgrade-operation-modal");
    await expect(modal).toBeVisible();
    await expect(modal).toContainText(`Upgrade Fleet to ${version}`);
    await expect(modal).toContainText("Fleet will validate and build this exact release");
  }

  async confirmUpgradeToVersion(version: string) {
    await this.page.getByRole("button", { name: `Confirm upgrade to ${version}`, exact: true }).click();
  }

  async validateUpgradeModalText(text: string) {
    await expect(this.page.getByTestId("upgrade-operation-modal")).toContainText(text);
  }

  private rowByLabel(label: string) {
    return this.page.getByText(label, { exact: true }).locator("xpath=..");
  }
}
