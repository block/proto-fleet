import { expect } from "@playwright/test";
import { BasePage } from "./base";

type Scope = "site" | "building";

export class FleetLocationsPage extends BasePage {
  async navigateToSitesPage() {
    await this.navigateToFleetPage();
    await this.page.getByTestId("fleet-tab-sites-activate").click();
    await this.validateSitesPageOpened();
  }

  async navigateToBuildingsPage() {
    await this.navigateToSitesPage();
    await this.page.getByTestId("fleet-tab-buildings-activate").click();
    await expect(this.page).toHaveURL(/\/fleet\/buildings(?:[?#].*)?$/);
    await expect(this.page.getByTestId("fleet-buildings-page")).toBeVisible();
  }

  async createSite(name: string): Promise<bigint> {
    await this.navigateToSitesPage();
    await this.clickAddSiteButton();
    await this.page.getByTestId("site-settings-name-input").fill(name);
    await this.page.getByTestId("site-settings-modal-continue").click();
    await this.waitForModalToClose("site-settings-modal");

    const saveSiteButton = this.page.getByTestId(
      this.isMobile ? "manage-site-modal-save-mobile" : "manage-site-modal-save",
    );
    await expect(saveSiteButton).toBeVisible();
    await saveSiteButton.click();
    await this.waitForModalToClose("full-screen-two-pane-modal");

    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();
    return await this.getScopeIdFromRowName(name, "site");
  }

  async createBuilding(siteName: string, buildingName: string): Promise<bigint> {
    await this.navigateToBuildingsPage();
    await this.clickAddBuildingButton();
    await this.page.getByTestId("building-settings-site-select").click();
    await this.page.getByRole("option", { name: siteName, exact: true }).click();
    await this.page.getByTestId("building-settings-name-input").fill(buildingName);
    await this.page.getByTestId("building-settings-modal-save").click();

    await expect(this.page.getByTestId("building-settings-modal")).toHaveCount(0);

    const fullScreenModal = this.page.getByTestId("full-screen-two-pane-modal");
    if (await fullScreenModal.isVisible().catch(() => false)) {
      await fullScreenModal.getByTestId("header-icon-button").click();
      await expect(fullScreenModal).toHaveCount(0);
    }

    const row = this.getListRowByName(buildingName);
    await expect(row).toBeVisible();
    return await this.getScopeIdFromRowName(buildingName, "building");
  }

  async openSiteDetail(name: string): Promise<bigint> {
    await this.navigateToSitesPage();
    const siteId = await this.getScopeIdFromRowName(name, "site");
    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();
    await row.getByTestId("name").click();
    await expect(this.page).toHaveURL(new RegExp(`/sites/${siteId.toString()}(?:[?#].*)?$`));
    await expect(this.page.getByTestId("site-detail-page")).toBeVisible();
    return siteId;
  }

  async validateSitesPageOpened() {
    await expect(this.page).toHaveURL(/\/fleet\/sites(?:[?#].*)?$/);
    await this.validateSitesListVisible();
  }

  async validateSitesListVisible() {
    await expect(this.page.getByTestId("fleet-sites-redirecting")).toHaveCount(0);
    await expect(this.page.getByTestId("fleet-sites-page")).toBeVisible();
    await this.selectAllSitesIfNeeded();
  }

  async validateSiteDetailOpened(siteName: string) {
    await expect(this.page.getByTestId("site-detail-page")).toBeVisible();
    await expect(this.page.getByTestId("site-detail-title")).toHaveText(siteName);
  }

  async validateSiteDetailMetrics(expected: { location?: string; buildings?: number }) {
    const metricsRow = this.page.getByTestId("site-detail-metrics-row");
    await expect(metricsRow).toBeVisible();

    if (expected.location !== undefined) {
      await expect(metricsRow.getByTestId("site-metric-location-value")).toHaveText(expected.location);
    }

    if (expected.buildings !== undefined) {
      await expect(metricsRow.getByTestId("site-metric-buildings-value")).toHaveText(String(expected.buildings));
    }
  }

  async editSiteDetailsFromDetail(updates: { name?: string; city?: string; powerCapacityMw?: string }) {
    await expect(this.page.getByTestId("site-detail-edit")).toBeVisible();
    await this.page.getByTestId("site-detail-edit").click();
    const fullScreenModal = this.page.getByTestId("full-screen-two-pane-modal");
    await expect(fullScreenModal).toBeVisible();
    await this.clickManageSiteEditDetails(fullScreenModal);

    const settingsModal = this.page.getByTestId("site-settings-modal");
    await expect(settingsModal).toBeVisible();

    if (updates.name !== undefined) {
      await settingsModal.getByTestId("site-settings-name-input").fill(updates.name);
    }

    if (updates.city !== undefined) {
      await settingsModal.getByTestId("site-settings-city-input").fill(updates.city);
    }

    if (updates.powerCapacityMw !== undefined) {
      await settingsModal.getByTestId("site-settings-capacity-input").fill(updates.powerCapacityMw);
    }

    await settingsModal.getByTestId("site-settings-modal-save").click();
    await this.waitForModalToClose("site-settings-modal");
    await this.closeFullScreenModalIfVisible();
  }

  async addBuildingFromSiteDetail(buildingName: string) {
    await expect(this.page.getByTestId("site-detail-add-building")).toBeVisible();
    await this.page.getByTestId("site-detail-add-building").click();

    const settingsModal = this.page.getByTestId("building-settings-modal");
    await expect(settingsModal).toBeVisible();
    await settingsModal.getByTestId("building-settings-name-input").fill(buildingName);
    await settingsModal.getByTestId("building-settings-modal-save").click();
    await this.waitForModalToClose("building-settings-modal");
  }

  async validateSiteDetailBuildingVisible(buildingName: string) {
    await expect(this.page.getByTestId("site-detail-page").getByText(buildingName, { exact: true })).toBeVisible();
  }

  async switchSiteDetailBreadcrumbTo(siteName: string) {
    const switcher = this.page.getByTestId("site-detail-breadcrumb-switcher");
    await expect(switcher).toBeVisible();
    await switcher.click();
    await this.page.getByTestId(`site-detail-breadcrumb-menu-item-${siteName}`).click();
    await expect(this.page.getByTestId("site-detail-breadcrumb-switcher")).toContainText(siteName);
    await expect(this.page.getByTestId("site-detail-title")).toHaveText(siteName);
  }

  async deleteSiteFromDetail() {
    await expect(this.page.getByTestId("site-detail-edit")).toBeVisible();
    await this.page.getByTestId("site-detail-edit").click();
    await this.clickManageSiteDelete();

    const deleteDialog = this.page.getByTestId("site-delete-dialog");
    const confirmDeleteButton = this.page.getByTestId("site-delete-dialog-confirm");
    await expect(confirmDeleteButton).toBeVisible();
    await confirmDeleteButton.click({ trial: true });
    await confirmDeleteButton.click();
    await expect(deleteDialog).toHaveCount(0);
    await expect(this.page.getByTestId("full-screen-two-pane-modal")).toHaveCount(0);
  }

  async validateSiteNotVisible(name: string) {
    await this.navigateToSitesPage();
    await expect(this.getListRowByName(name)).toHaveCount(0);
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

  async deleteBuilding(name: string) {
    await this.navigateToBuildingsPage();
    await this.openManageBuildingFromList(name);
    await this.clickManageBuildingDelete();

    const confirmDeleteButton = this.page.getByTestId("building-delete-dialog-confirm");
    await expect(confirmDeleteButton).toBeVisible();
    await confirmDeleteButton.click({ trial: true });
    await confirmDeleteButton.click();
    await expect(this.getListRowByName(name)).toHaveCount(0);
  }

  async renameBuilding(currentName: string, nextName: string) {
    await this.navigateToBuildingsPage();
    const fullScreenModal = await this.openManageBuildingFromList(currentName);
    await this.clickManageBuildingEditDetails(fullScreenModal);

    const settingsModal = this.page.getByTestId("building-settings-modal");
    await expect(settingsModal).toBeVisible();
    await settingsModal.getByTestId("building-settings-name-input").fill(nextName);
    await settingsModal.getByTestId("building-settings-modal-save").click();
    await this.waitForModalToClose("building-settings-modal");

    await this.closeFullScreenModalIfVisible();
    await expect(this.getListRowByName(nextName)).toBeVisible();
    await expect(this.getListRowByName(currentName)).toHaveCount(0);
  }

  async removeRackFromBuilding(buildingName: string, rackId: bigint) {
    await this.navigateToBuildingsPage();
    const fullScreenModal = await this.openManageBuildingFromList(buildingName);
    await fullScreenModal.getByTestId(`manage-building-remove-rack-${rackId.toString()}`).click();
    await this.clickVisibleManageBuildingAction("manage-building-save", fullScreenModal);
    await this.waitForModalToClose("full-screen-two-pane-modal");
  }

  async deleteSiteByNameIfVisible(name: string) {
    await this.navigateToSitesPage();
    if (
      !(await this.getListRowByName(name)
        .isVisible()
        .catch(() => false))
    ) {
      return;
    }

    await this.deleteSite(name);
  }

  async deleteBuildingByNameIfVisible(name: string) {
    await this.navigateToBuildingsPage();
    if (
      !(await this.getListRowByName(name)
        .isVisible()
        .catch(() => false))
    ) {
      return;
    }

    await this.deleteBuilding(name);
  }

  async listSiteNames(): Promise<string[]> {
    await this.navigateToSitesPage();
    return await this.listVisibleRowNames();
  }

  async listBuildingNames(): Promise<string[]> {
    await this.navigateToBuildingsPage();
    return await this.listVisibleRowNames();
  }

  async validateSiteRowCounts(
    siteName: string,
    expected: {
      buildings: number;
      racks: number;
      miners: number;
    },
  ) {
    await this.navigateToSitesPage();
    const row = this.getListRowByName(siteName);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("buildings")).toHaveText(String(expected.buildings));
    await expect(row.getByTestId("racks")).toHaveText(String(expected.racks));
    await expect(row.getByTestId("miners")).toHaveText(String(expected.miners));
  }

  async validateBuildingRowCounts(
    buildingName: string,
    expected: {
      siteName: string;
      racks: number;
      miners: number;
    },
  ) {
    await this.navigateToBuildingsPage();
    const row = this.getListRowByName(buildingName);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("site")).toHaveText(expected.siteName);
    await expect(row.getByTestId("racks")).toHaveText(String(expected.racks));
    await expect(row.getByTestId("miners")).toHaveText(String(expected.miners));
  }

  private getListRowByName(name: string) {
    return this.page
      .getByTestId("list-row")
      .filter({ has: this.page.getByTestId("name").getByText(name, { exact: true }) })
      .first();
  }

  private async listVisibleRowNames(): Promise<string[]> {
    const nameCells = this.page.getByTestId("list-row").getByTestId("name");
    const count = await nameCells.count();
    const names: string[] = [];

    for (let i = 0; i < count; i++) {
      names.push((await nameCells.nth(i).innerText()).trim());
    }

    return names;
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
    if (!this.isMobile) {
      await this.page.getByTestId("manage-site-modal-delete").click();
      return;
    }

    const overflowMenu = await this.openFullScreenOverflowMenu();
    await overflowMenu.getByTestId("manage-site-modal-delete-overflow-item").click();
  }

  private async clickManageSiteEditDetails(scope = this.page.getByTestId("full-screen-two-pane-modal")) {
    if (!this.isMobile) {
      await scope.getByTestId("manage-site-modal-edit-details").click();
      return;
    }

    const overflowMenu = await this.openFullScreenOverflowMenu();
    await overflowMenu.getByTestId("manage-site-modal-edit-details-overflow-item").click();
  }

  private async clickManageBuildingDelete() {
    if (!this.isMobile) {
      await this.page.getByTestId("manage-building-delete").click();
      return;
    }

    const overflowMenu = await this.openFullScreenOverflowMenu();
    await overflowMenu.getByTestId("manage-building-delete-overflow-item").click();
  }

  private async clickManageBuildingEditDetails(scope = this.page.getByTestId("full-screen-two-pane-modal")) {
    if (!this.isMobile) {
      await scope.getByTestId("manage-building-edit-details").click();
      return;
    }

    const overflowMenu = await this.openFullScreenOverflowMenu();
    await overflowMenu.getByTestId("manage-building-edit-details-overflow-item").click();
  }

  private async openFullScreenOverflowMenu() {
    const overflowTrigger = this.page.getByTestId("full-screen-two-pane-modal").getByTestId("overflow-menu-trigger");
    await expect(overflowTrigger).toBeVisible();
    await overflowTrigger.click();
    const overflowMenu = this.page.getByTestId("full-screen-overflow-sheet");
    await expect(overflowMenu).toBeVisible();
    return overflowMenu;
  }

  private async closeFullScreenModalIfVisible() {
    const fullScreenModal = this.page.getByTestId("full-screen-two-pane-modal");
    if (!(await fullScreenModal.isVisible().catch(() => false))) {
      return;
    }

    await fullScreenModal.getByTestId("header-icon-button").click();
    await expect(fullScreenModal).toHaveCount(0);
  }

  private async waitForModalToClose(testId: string) {
    await expect(this.page.getByTestId(testId)).toHaveCount(0);
  }

  private async openRowActions(name: string) {
    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: `Actions for ${name}`, exact: true }).click();
  }

  private async clickRowAction(label: string) {
    await this.page.getByText(label, { exact: true }).click();
  }

  private async openManageBuildingFromList(name: string) {
    await this.openRowActions(name);
    await this.clickRowAction("Edit building");
    const fullScreenModal = this.page.getByTestId("full-screen-two-pane-modal");
    await expect(fullScreenModal).toBeVisible();
    return fullScreenModal;
  }

  private async clickVisibleManageBuildingAction(
    testId: "manage-building-save",
    scope = this.page.getByTestId("full-screen-two-pane-modal"),
  ) {
    await scope.getByTestId(this.isMobile ? `${testId}-mobile` : testId).click();
  }

  private async getScopeIdFromRowName(name: string, scope: Scope): Promise<bigint> {
    const row = this.getListRowByName(name);
    await expect(row).toBeVisible();

    const trigger = row.getByRole("button", { name: `Actions for ${name}`, exact: true });
    await expect(trigger).toBeVisible();

    const testId = await trigger.getAttribute("data-testid");
    const capturedId = testId?.match(new RegExp(`^${scope}-list-row-(\\d+)-actions-trigger$`))?.[1];

    if (!capturedId) {
      throw new Error(`Could not parse ${scope} id from row action trigger: ${testId ?? "missing test id"}`);
    }

    return BigInt(capturedId);
  }
}
