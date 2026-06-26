import { expect } from "@playwright/test";
import { BasePage } from "./base";

type Scope = "site" | "building" | "rack";

export class FleetLocationsPage extends BasePage {
  async navigateToSitesPage() {
    await this.page.goto("/fleet/sites");
    await expect(this.page).toHaveURL(/\/fleet\/sites(?:[?#].*)?$/);
    await expect(this.page.getByTestId("fleet-sites-redirecting")).toHaveCount(0);
    await expect(this.page.getByTestId("fleet-sites-page")).toBeVisible();
    await this.selectAllSitesIfNeeded();

    const sitePickerTrigger = this.page.getByTestId("site-picker-trigger");
    if (await sitePickerTrigger.isVisible().catch(() => false)) {
      await this.page.goto("/fleet/sites");
      await expect(this.page).toHaveURL(/\/fleet\/sites(?:[?#].*)?$/);
      await expect(this.page.getByTestId("fleet-sites-redirecting")).toHaveCount(0);
      await expect(this.page.getByTestId("fleet-sites-page")).toBeVisible();
      await expect(sitePickerTrigger).toContainText("All sites");
    }
  }

  async navigateToBuildingsPage() {
    await this.navigateToSitesPage();
    await this.page.goto("/fleet/buildings");
    await expect(this.page).toHaveURL(/\/fleet\/buildings(?:[?#].*)?$/);
    await expect(this.page.getByTestId("fleet-buildings-page")).toBeVisible();
  }

  async createSite(name: string): Promise<bigint> {
    await this.navigateToSitesPage();
    await this.clickAddSiteButton();
    await this.page.getByTestId("site-settings-name-input").fill(name);
    await this.page.getByTestId("site-settings-modal-continue").click();

    const saveSiteButton = this.page.locator('[data-testid="manage-site-modal-save"]:visible');
    await expect(saveSiteButton).toBeVisible();
    await saveSiteButton.click();

    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();
    return await this.getScopeIdFromRowName(name, "site");
  }

  async deleteSite(name: string) {
    await this.navigateToSitesPage();
    await this.openRowActions(name);
    await this.clickRowAction("Edit site");
    await this.clickManageSiteDelete();

    const confirmDeleteButton = this.page.getByTestId("site-delete-dialog-confirm");
    await expect(confirmDeleteButton).toBeVisible();
    await confirmDeleteButton.click({ trial: true });
    await confirmDeleteButton.click();
    await expect(this.getListRowByName(name)).toHaveCount(0);
  }

  async createBuilding(siteName: string, buildingName: string): Promise<bigint> {
    await this.navigateToBuildingsPage();
    await this.clickAddBuildingButton();
    await this.page.getByTestId("building-settings-site-select").click();
    await this.page.getByRole("option", { name: siteName, exact: true }).click();
    await this.page.getByTestId("building-settings-name-input").fill(buildingName);
    await this.page.getByTestId("building-settings-modal-save").click();

    const row = this.getListRowByName(buildingName);
    await expect(row).toBeVisible();
    return await this.getScopeIdFromRowName(buildingName, "building");
  }

  async deleteBuilding(name: string) {
    await this.navigateToBuildingsPage();
    await this.openRowActions(name);
    await this.clickRowAction("Edit building");
    await this.clickManageBuildingDelete();

    const confirmDeleteButton = this.page.getByTestId("building-delete-dialog-confirm");
    await expect(confirmDeleteButton).toBeVisible();
    await confirmDeleteButton.click({ trial: true });
    await confirmDeleteButton.click();
    await expect(this.getListRowByName(name)).toHaveCount(0);
  }

  async validateSiteMinerCount(siteName: string, expectedCount: number) {
    await this.navigateToSitesPage();
    await expect(this.getListRowByName(siteName).getByTestId("miners")).toHaveText(String(expectedCount));
  }

  private getListRowByName(name: string) {
    return this.page
      .getByTestId("list-row")
      .filter({ has: this.page.getByTestId("name").getByText(name, { exact: true }) })
      .first();
  }

  private async selectAllSitesIfNeeded() {
    const sitePickerTrigger = this.page.getByTestId("site-picker-trigger");
    if (!(await sitePickerTrigger.isVisible().catch(() => false))) {
      await expect(this.page.getByTestId("fleet-sites-page")).toBeVisible();
      return;
    }

    const currentLabel = (await sitePickerTrigger.textContent())?.trim();
    if (currentLabel === "All sites") {
      return;
    }

    await sitePickerTrigger.click();
    const allSitesOption = this.page.getByTestId("site-picker-option-all");
    await expect(allSitesOption).toBeVisible();
    await allSitesOption.click();
    await expect(sitePickerTrigger).toContainText("All sites");
  }

  private async clickAddSiteButton() {
    const headerAddSiteButton = this.page.getByTestId("fleet-sites-add");
    if (await headerAddSiteButton.isVisible().catch(() => false)) {
      await headerAddSiteButton.click();
      return;
    }

    const emptyStateAddSiteButton = this.page.getByRole("button", { name: "Add a site", exact: true });
    await expect(emptyStateAddSiteButton).toBeVisible();
    await emptyStateAddSiteButton.click();
  }

  private async clickAddBuildingButton() {
    const headerAddBuildingButton = this.page.getByTestId("fleet-buildings-add");
    if (await headerAddBuildingButton.isVisible().catch(() => false)) {
      await headerAddBuildingButton.click();
      return;
    }

    const emptyStateAddBuildingButton = this.page.getByRole("button", { name: "Add building", exact: true });
    await expect(emptyStateAddBuildingButton).toBeVisible();
    await emptyStateAddBuildingButton.click();
  }

  private async clickManageSiteDelete() {
    const manageSiteDeleteButton = this.page.locator('[data-testid="manage-site-modal-delete"]:visible');
    if (await manageSiteDeleteButton.isVisible().catch(() => false)) {
      await manageSiteDeleteButton.click();
      return;
    }

    const siteSettingsDeleteButton = this.page.locator('[data-testid="site-settings-modal-delete"]:visible');
    if (await siteSettingsDeleteButton.isVisible().catch(() => false)) {
      await siteSettingsDeleteButton.click();
      return;
    }

    const overflowMenu = await this.openFullScreenOverflowMenu();
    const deleteSiteAction = overflowMenu.getByText("Delete site", { exact: true });
    if (await deleteSiteAction.isVisible().catch(() => false)) {
      await deleteSiteAction.click();
      return;
    }

    await overflowMenu.getByText("Site settings", { exact: true }).click();
    await expect(siteSettingsDeleteButton).toBeVisible();
    await siteSettingsDeleteButton.click();
  }

  private async clickManageBuildingDelete() {
    const deleteButton = this.page.locator('[data-testid="manage-building-delete"]:visible');
    if (await deleteButton.isVisible().catch(() => false)) {
      await deleteButton.click();
      return;
    }

    const overflowMenu = await this.openFullScreenOverflowMenu();
    await overflowMenu.getByText("Delete building", { exact: true }).click();
  }

  private async openFullScreenOverflowMenu() {
    const overflowTrigger = this.page.getByTestId("full-screen-two-pane-modal").getByTestId("overflow-menu-trigger");
    await expect(overflowTrigger).toBeVisible();
    await overflowTrigger.click();
    return this.page.locator("div.fixed.inset-0.z-60");
  }

  private async openRowActions(name: string) {
    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();
    await row.locator('button[data-testid$="-actions-trigger"]').first().click();
  }

  private async clickRowAction(label: string) {
    await this.page.getByText(label, { exact: true }).click();
  }

  private async getScopeIdFromRowName(name: string, scope: Scope): Promise<bigint> {
    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();

    const trigger = row.locator('button[data-testid$="-actions-trigger"]').first();
    await expect(trigger).toBeVisible();

    const testId = await trigger.getAttribute("data-testid");
    const pattern =
      scope === "rack"
        ? /^rack-list-row-(\d+)-actions-trigger$/
        : new RegExp(`^${scope}-list-row-(\\d+)-actions-trigger$`);
    const capturedId = testId?.match(pattern)?.[1];

    if (!capturedId) {
      throw new Error(`Could not parse ${scope} id from row action trigger: ${testId ?? "missing test id"}`);
    }

    return BigInt(capturedId);
  }
}
