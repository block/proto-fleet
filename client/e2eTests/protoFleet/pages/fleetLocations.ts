import { expect, type Locator } from "@playwright/test";
import { BasePage } from "./base";

export class FleetLocationsPage extends BasePage {
  async navigateToSitesPage() {
    await this.page.goto("/settings/sites");
    await expect(this.page).toHaveURL(/.*\/settings\/sites/);
    await expect(this.page.getByTestId("settings-sites-page")).toBeVisible();
  }

  async createSite(siteName: string): Promise<string> {
    await this.page.getByRole("button", { name: "Add a site", exact: true }).first().click();
    await expect(this.page.getByText("Site settings", { exact: true })).toBeVisible();
    await this.page.locator("#site-settings-name").fill(siteName);
    await this.page.getByRole("button", { name: "Continue", exact: true }).click();
    await this.page.locator('[data-testid="manage-site-modal-save"]:visible').click();
    await this.validateTextInToast(`Site "${siteName}" created`);
    const row = this.getSiteRow(siteName);
    await expect(row).toBeVisible();
    const testId = await row.getAttribute("data-testid");

    if (!testId) {
      throw new Error(`Could not read site row test id for "${siteName}".`);
    }

    return testId.replace("sites-all-table-row-", "");
  }

  async openSiteSettings(siteName: string) {
    await this.getSiteRow(siteName).click();
    const siteSettingsView = this.page.getByTestId("site-settings-single-view");
    await expect(siteSettingsView).toBeVisible();
    await expect(siteSettingsView).toContainText(siteName);
  }

  async returnToAllSites() {
    await this.page.getByTestId("site-settings-back-to-all").click();
    await expect(this.page.getByTestId("sites-all-table")).toBeVisible();
  }

  async createBuildingInSelectedSite(buildingName: string): Promise<string> {
    await expect(this.page.getByTestId("site-settings-single-view")).toBeVisible();
    await this.page.getByTestId("site-settings-add-building").click();
    await expect(this.page.getByText("Building settings", { exact: true })).toBeVisible();
    await this.page.locator("#building-settings-name").fill(buildingName);
    await this.page.getByTestId("building-settings-modal-save").click();
    await this.validateTextInToast(`Building "${buildingName}" created`);
    const row = this.getSingleSiteBuildingRow(buildingName);
    await expect(row).toBeVisible();
    const testId = await row.getAttribute("data-testid");

    if (!testId) {
      throw new Error(`Could not read building row test id for "${buildingName}".`);
    }

    return testId.replace("site-settings-building-row-", "");
  }

  private getSiteRow(siteName: string): Locator {
    return this.page.getByTestId("sites-all-table").getByRole("button").filter({ hasText: siteName }).first();
  }

  private getSingleSiteBuildingRow(buildingName: string): Locator {
    return this.page.getByTestId("site-settings-single-view").getByRole("button").filter({ hasText: buildingName });
  }
}
