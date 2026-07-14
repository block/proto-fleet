import { expect, type Locator, Page } from "@playwright/test";
import { DEFAULT_TIMEOUT, testConfig } from "../config/test.config";

const FLEET_TAB_ROUTE = /.*\/fleet\/(?:sites|buildings|racks|miners)(?:[/?#].*)?$/;

export class BasePage {
  constructor(
    protected page: Page,
    protected isMobile: boolean = false,
  ) {}

  async reloadPage() {
    await this.page.reload();
  }

  async validateActiveFilter(filterLabel: string) {
    await expect(this.activeFilterEditButton(filterLabel)).toBeVisible();
  }

  async validateActiveFilterSummary(filterValue: string, expectedSummary: string) {
    await expect(await this.visibleTestIdLocator(`active-filter-${filterValue}-edit`)).toHaveText(expectedSummary);
  }

  async validateActiveFilterNotVisible(filterLabel: string) {
    await expect(this.activeFilterEditButton(filterLabel)).toHaveCount(0);
  }

  async validateNoResultsEmptyState() {
    await expect(this.page.getByText("No results", { exact: true })).toBeVisible();
    await expect(this.page.getByRole("button", { name: "Clear all filters", exact: true })).toBeVisible();
  }

  async clickClearAllFilters() {
    await this.page.getByRole("button", { name: "Clear all filters", exact: true }).click();
  }

  async clearActiveFilter(filterValue: string) {
    if (!this.isMobile) {
      const clearButton = await this.visibleTestIdLocator(`active-filter-${filterValue}-clear`);
      await clearButton.scrollIntoViewIfNeeded();
      await clearButton.click();
      return;
    }

    const editButton = await this.visibleTestIdLocator(`active-filter-${filterValue}-edit`);
    await editButton.click();

    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();

    const options = popover.locator('[data-testid^="filter-option-"]');
    const count = await options.count();

    for (let i = 0; i < count; i++) {
      const option = options.nth(i);
      const checkbox = option.locator('input[type="checkbox"]');
      if (await checkbox.isChecked().catch(() => false)) {
        await option.click();
      }
    }

    await this.dismissMobilePopoverSheet("dropdown-filter-popover");
    await expect(popover).toBeHidden();
  }

  async clickNewSavedViewButton() {
    const emptyState = this.viewsEmptyStateNewButton();
    if (await emptyState.isVisible().catch(() => false)) {
      await emptyState.click();
      return;
    }

    await this.openViewsPopover();
    await this.page.getByTestId("fleet-view-tabs-popover-new-view").click();
  }

  async clickClearActiveView() {
    await this.openViewsPopover();
    await this.page.getByTestId("fleet-view-tabs-popover-clear-view").click();
  }

  async validateViewModalOpened(title: "New view" | "Update view" | "Rename view") {
    const modal = this.page.getByTestId("view-modal");
    await expect(modal).toBeVisible();
    await expect(modal).toContainText(title);
  }

  async inputViewName(name: string) {
    await this.page.locator("#view-name").fill(name);
  }

  async saveNewView() {
    await this.page.getByTestId("view-modal").getByRole("button", { name: "Save", exact: true }).click();
    await expect(this.page.getByTestId("view-modal")).toBeHidden();
  }

  async updateSavedView() {
    await this.page.getByTestId("view-modal").getByRole("button", { name: "Update", exact: true }).click();
    await expect(this.page.getByTestId("view-modal")).toBeHidden();
  }

  async confirmRenameView() {
    await this.page.getByTestId("view-modal").getByRole("button", { name: "Rename", exact: true }).click();
    await expect(this.page.getByTestId("view-modal")).toBeHidden();
  }

  async validateViewTabVisible(viewName: string) {
    await this.openViewsPopover();
    await expect(this.viewRow(viewName)).toBeVisible();
    if (this.isMobile) {
      await this.dismissMobilePopoverSheet("fleet-view-tabs-views-popover");
    } else {
      await this.fleetViewTabsTrigger().click();
    }
    await expect(this.viewsPopover()).toBeHidden();
  }

  async validateViewTabActive(viewName: string) {
    await expect(this.fleetViewTabsTrigger()).toContainText(viewName);
  }

  async clickViewTab(viewName: string) {
    await this.openViewsPopover();
    await this.viewRow(viewName).click();
  }

  async clickResetViewAction(viewName: string) {
    await this.validateViewTabActive(viewName);
    await this.openKebabPopover();
    await this.page.getByTestId("fleet-view-tabs-reset-action").click();
  }

  async clickUpdateViewAction(viewName: string) {
    await this.validateViewTabActive(viewName);
    await this.openKebabPopover();
    await this.page.getByTestId("fleet-view-tabs-update-action").click();
  }

  async clickRenameViewAction(viewName: string) {
    await this.validateViewTabActive(viewName);
    await this.openKebabPopover();
    await this.page.getByTestId("fleet-view-tabs-rename-action").click();
  }

  async clickDeleteViewAction(viewName: string) {
    await this.validateViewTabActive(viewName);
    await this.openKebabPopover();
    await this.page.getByTestId("fleet-view-tabs-delete-action").click();
  }

  async validateViewTabNotVisible(viewName: string) {
    const trigger = this.fleetViewTabsTrigger();
    if (await trigger.isVisible().catch(() => false)) {
      await expect(trigger).not.toContainText(viewName);
      await trigger.click();
      const popover = this.viewsPopover();
      if (await popover.isVisible().catch(() => false)) {
        await expect(this.viewRow(viewName)).toHaveCount(0);
        if (this.isMobile) {
          await this.dismissMobilePopoverSheet("fleet-view-tabs-views-popover");
        } else {
          await trigger.click();
        }
        await expect(popover).toBeHidden();
      }
      return;
    }

    await expect(this.viewsEmptyStateNewButton()).toBeVisible();
  }

  async validateDeleteViewDialogOpened(viewName: string) {
    const dialog = this.page.getByTestId("fleet-view-tabs-delete-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(`Delete the view "${viewName}"? This can't be undone.`);
  }

  async confirmDeleteView() {
    const dialog = this.page.getByTestId("fleet-view-tabs-delete-dialog");
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog).toBeHidden();
  }

  protected async setNestedCheckboxFilterSelection(categoryKey: string, targetLabels: string[]) {
    if (targetLabels.length !== 1) {
      throw new Error(
        `Expected exactly one target label for "${categoryKey}" filter, received ${targetLabels.length}.`,
      );
    }

    const [targetLabel] = targetLabels;
    const activeEditButton = await this.findVisibleTestIdLocator(`active-filter-${categoryKey}-edit`);
    let clearedExistingSelection = false;
    if (activeEditButton) {
      const currentSummary = ((await activeEditButton.textContent()) ?? "").replace(/\s+/g, " ").trim();
      if (currentSummary === targetLabel) {
        return;
      }

      await this.clearActiveFilter(categoryKey);
      await this.waitForActiveFilterToClear(categoryKey);
      clearedExistingSelection = true;
    }

    const addFilterPopover = await this.openVisibleAddFilter();
    const submenu = await this.openNestedFilterSubmenu(addFilterPopover, categoryKey);
    await this.waitForCheckboxFilterOptions(submenu, categoryKey, targetLabels);
    if (clearedExistingSelection) {
      await this.waitForCheckboxFilterSelectionState(submenu, categoryKey, []);
    }
    const targetOption = (await this.readCheckboxFilterOptionStates(submenu)).find(
      ({ label }) => label === targetLabel,
    );
    if (!targetOption) {
      throw new Error(`Could not find "${targetLabel}" in the visible "${categoryKey}" filter options.`);
    }

    await (await this.visibleContainerTestIdLocator(submenu, `filter-option-${targetOption.id}`)).click();
    await this.dismissNestedAddFilterPopover();
  }

  protected async toggleAllNestedCheckboxFilterOptions(categoryKey: string) {
    const activeEditButton = await this.findVisibleTestIdLocator(`active-filter-${categoryKey}-edit`);
    if (activeEditButton) {
      await this.clearActiveFilter(categoryKey);
      return;
    }

    const addFilterPopover = await this.openVisibleAddFilter();
    const submenu = await this.openNestedFilterSubmenu(addFilterPopover, categoryKey);
    await this.toggleVisibleCheckboxFilterOptions(submenu);
    await this.dismissNestedAddFilterPopover();
  }

  private async toggleVisibleCheckboxFilterOptions(container: Locator) {
    const options = container.locator('[data-testid^="filter-option-"]');
    const count = await options.count();
    if (count === 0) {
      return;
    }

    let anyChecked = false;
    for (let i = 0; i < count; i++) {
      if (
        await options
          .nth(i)
          .locator('input[type="checkbox"]')
          .isChecked()
          .catch(() => false)
      ) {
        anyChecked = true;
        break;
      }
    }

    for (let i = 0; i < count; i++) {
      const option = options.nth(i);
      const isChecked = await option
        .locator('input[type="checkbox"]')
        .isChecked()
        .catch(() => false);
      if (isChecked === anyChecked) {
        await option.click();
      }
    }
  }

  private async waitForCheckboxFilterOptions(container: Locator, categoryKey: string, targetLabels: string[]) {
    await expect
      .poll(
        async () => {
          const visibleOptions = await this.readCheckboxFilterOptionStates(container);
          const visibleLabels = new Set(visibleOptions.map(({ label }) => label));
          return targetLabels.filter((label) => !visibleLabels.has(label));
        },
        {
          timeout: DEFAULT_TIMEOUT,
          message: `Expected the visible "${categoryKey}" filter options to include: ${targetLabels.join(", ")}.`,
        },
      )
      .toEqual([]);
  }

  private async waitForCheckboxFilterSelectionState(
    container: Locator,
    categoryKey: string,
    expectedCheckedLabels: string[],
  ) {
    const expected = [...expectedCheckedLabels].sort();
    await expect
      .poll(
        async () =>
          (await this.readCheckboxFilterOptionStates(container))
            .filter(({ checked }) => checked)
            .map(({ label }) => label)
            .sort(),
        {
          timeout: DEFAULT_TIMEOUT,
          message: `Expected the visible "${categoryKey}" filter selection to be ${expected.join(", ") || "empty"}.`,
        },
      )
      .toEqual(expected);
  }

  private async readCheckboxFilterOptionStates(container: Locator) {
    const options = container.locator('[data-testid^="filter-option-"]');
    const count = await options.count();
    const visibleOptions = new Map<string, { id: string; label: string; checked: boolean }>();

    for (let i = 0; i < count; i++) {
      const option = options.nth(i);
      if (!(await option.isVisible().catch(() => false))) {
        continue;
      }

      const testId = await option.getAttribute("data-testid");
      if (!testId) {
        continue;
      }

      const id = testId.replace(/^filter-option-/, "");
      visibleOptions.set(id, {
        id,
        label: ((await option.textContent()) ?? "").replace(/\s+/g, " ").trim(),
        checked: await option
          .locator('input[type="checkbox"]')
          .isChecked()
          .catch(() => false),
      });
    }

    return [...visibleOptions.values()];
  }

  async validateLoggedIn(timeout: number = DEFAULT_TIMEOUT) {
    if (this.isMobile) {
      await expect(this.page.getByTestId("navigation-menu-button")).toBeVisible({ timeout });
    } else {
      await expect(this.page.getByTestId("logout-button")).toBeVisible({ timeout });
    }
  }

  async logout() {
    const logoutButton = this.page.getByTestId("logout-button");
    const navigationMenuButton = this.page.getByTestId("navigation-menu-button");

    // The current session can be invalidated server-side at any moment
    // (e.g. cleanup deactivated the member this page is logged in as),
    // making the app redirect to /auth on its own mid-logout. Treat
    // either outcome — a successful logout click or the app's own
    // redirect — as logged out instead of timing out on a button that
    // no longer exists.
    await expect(async () => {
      if (this.page.url().includes("/auth")) {
        return;
      }
      if (this.isMobile && !(await logoutButton.isVisible().catch(() => false))) {
        await navigationMenuButton.click({ timeout: 2_000 });
      }
      await logoutButton.click({ timeout: 2_000 });
    }).toPass({ timeout: DEFAULT_TIMEOUT });
  }

  async validateTitle(expectedTitle: string) {
    const titleLocator = this.page.locator(`//*[contains(@class,'heading')][text()='${expectedTitle}']`);
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleInModal(expectedTitle: string) {
    const titleLocator = this.page.locator(
      `//*[@data-testid='modal']//*[contains(@class,'heading')][text()='${expectedTitle}']`,
    );
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleNotVisible(expectedTitle: string) {
    const titleLocator = this.page.locator(`//*[contains(@class,'heading')][text()='${expectedTitle}']`);
    await expect(titleLocator).toBeHidden();
  }

  async validateTitleInModalNotVisible(expectedTitle: string) {
    const titleLocator = this.page.locator(
      `//*[@data-testid='modal']//*[contains(@class,'heading')][text()='${expectedTitle}']`,
    );
    await expect(titleLocator).toBeHidden();
  }

  async validateTextIsVisible(text: string) {
    await expect(this.page.getByText(text)).toBeVisible();
  }

  async validateTextInToast(text: string, timeout: number = DEFAULT_TIMEOUT) {
    const toast = this.page.getByTestId("toast").getByText(text);
    await expect(toast).toBeVisible({ timeout });
  }

  async validateTextInToastGroup(text: string) {
    const groupedHeaderMessage = this.page.getByTestId("grouped-toaster-header").getByText(text).first();
    const groupedBodyMessage = this.page.getByTestId("toaster-container").getByTestId("toast").getByText(text).first();

    await expect
      .poll(
        async () =>
          (await groupedHeaderMessage.isVisible().catch(() => false)) ||
          (await groupedBodyMessage.isVisible().catch(() => false)),
        {
          timeout: DEFAULT_TIMEOUT,
        },
      )
      .toBe(true);
  }

  async dismissToast() {
    const toast = this.page.getByTestId("toaster-container");
    const dismissButton = this.page.getByRole("button", { name: "Dismiss" });
    if (!(await dismissButton.isVisible())) {
      await toast.click();
    }
    await toast.getByRole("button", { name: "Dismiss" }).click();
  }

  async validateTextInModal(text: string) {
    await expect
      .poll(() => this.modalHasVisibleText(text), {
        message: `Expected "${text}" to be visible in the modal`,
        timeout: DEFAULT_TIMEOUT,
      })
      .toBe(true);
  }

  async validateTextNotInModal(text: string) {
    await expect
      .poll(() => this.modalHasVisibleText(text), {
        message: `Expected "${text}" not to be visible in the modal`,
        timeout: DEFAULT_TIMEOUT,
      })
      .toBe(false);
  }

  private async modalHasVisibleText(text: string) {
    const matches = this.page.getByTestId("modal").getByText(text);
    const count = await matches.count();
    for (let index = 0; index < count; index += 1) {
      if (
        await matches
          .nth(index)
          .isVisible()
          .catch(() => false)
      ) {
        return true;
      }
    }
    return false;
  }

  async validateButtonIsVisible(text: string) {
    await expect(this.page.getByRole("button", { name: text })).toBeVisible();
  }

  async clickNavigationMenuIfMobile() {
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu-button").click();
    }
  }

  async clickExpandSettingsIfMobile() {
    if (this.isMobile && !this.page.url().includes("/settings")) {
      await this.page.getByTestId("navigation-menu").getByText("Settings").click();
    }
  }

  async navigateToHomePage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/dashboard"]').click();
    await expect(this.page).toHaveURL(/.*\/dashboard$/);
  }

  async navigateToFleetPage() {
    if (
      FLEET_TAB_ROUTE.test(this.page.url()) &&
      (await this.page
        .getByTestId("fleet-layout")
        .isVisible()
        .catch(() => false))
    ) {
      return;
    }

    const fleetLink = this.page.getByTestId("navigation-menu").locator('a[href="/fleet"]');

    await this.clickNavigationMenuIfMobile();
    if (await fleetLink.isVisible().catch(() => false)) {
      await fleetLink.click();
    } else {
      await this.page.goto("/fleet/sites");
    }
    await expect(this.page.getByTestId("fleet-layout")).toBeVisible();
    await expect(this.page).toHaveURL(FLEET_TAB_ROUTE);
  }

  async navigateToMinersPage() {
    await this.navigateToFleetPage();
    await this.page.getByTestId("fleet-tab-miners-activate").click();
    await expect(this.page).toHaveURL(/.*\/fleet\/miners/);
  }

  async navigateToGroupsPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/groups"]').click();
    await expect(this.page).toHaveURL(/.*\/groups/);
  }

  async navigateToRacksPage() {
    await this.navigateToFleetPage();
    await this.page.getByTestId("fleet-tab-racks-activate").click();
    await expect(this.page).toHaveURL(/.*\/fleet\/racks/);
  }

  async navigateToActivityPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/activity"]').click();
    await expect(this.page).toHaveURL(/.*\/activity/);
  }

  async navigateToSettingsPage() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu").locator('a[href="/settings/network"]').click();
    } else {
      await this.page.getByTestId("navigation-menu").locator('a[href="/settings"]').click();
    }
    await expect(this.page).toHaveURL(/.*\/settings/);
  }

  async navigateSettingsIfDesktop() {
    // desktop can't navigate directly to subpages of settings
    if (!this.isMobile && !this.page.url().includes("/settings")) {
      await this.navigateToSettingsPage();
    }
  }

  async navigateToSecuritySettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/security"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/security/);
  }

  async navigateToNetworkSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/network"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/network/);
  }

  async navigateToPreferencesSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/preferences"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/preferences/);
  }

  async navigateToTeamSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/team"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/team/);
  }

  async navigateToMiningPoolsSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/mining-pools"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/mining-pools/);
  }

  async navigateToFirmwareSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/firmware"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/firmware/);
  }

  async navigateToApiKeysSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/integrations"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/integrations/);
  }

  async navigateToSchedulesSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/schedules"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/schedules/);
  }

  async navigateToCurtailmentSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/curtailment"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/curtailment/);
  }

  async navigateToAlertsSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/alerts"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/alerts/);
  }

  async navigateToServerLogsSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/server-logs"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/server-logs/);
  }

  async clickButton(text: string) {
    const visibleButton = await this.findVisibleButton(text);
    if (visibleButton) {
      await visibleButton.click();
      return;
    }

    if (this.isMobile && (await this.clickOverflowAction(text))) {
      return;
    }

    await this.page.getByRole("button", { name: text, disabled: false, exact: true }).click();
  }

  async clickUntilNotVisible(text: string) {
    const button = this.page.getByRole("button", { name: text, disabled: false, exact: true });

    await expect(button).toBeVisible();
    await expect(async () => {
      const isVisible = await button.isVisible();
      if (isVisible) {
        await button.click();
        throw new Error("Button still visible, looping until it is not or the time runs out");
      }
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [100] });
  }

  async clickIn(text: string, testId: string) {
    await this.page.getByTestId(testId).getByRole("button", { name: text, disabled: false, exact: true }).click();
  }

  async validateModalIsOpen() {
    await expect(this.page.getByTestId("modal")).toBeVisible();
  }

  async validateModalIsClosed() {
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  async dismissModalIfVisible() {
    const modal = this.page.getByTestId("modal");
    if (!(await modal.isVisible().catch(() => false))) {
      return;
    }

    const headerDismiss = modal.getByTestId("header-icon-button");
    if (await headerDismiss.isVisible().catch(() => false)) {
      await headerDismiss.click();
      await this.validateModalIsClosed();
      return;
    }

    await this.page.keyboard.press("Escape").catch(() => undefined);
    await this.validateModalIsClosed();
  }

  async clickSaveInModal() {
    await this.clickIn("Save", "modal");
  }

  // Helper method to try an action with timeout and return success/failure
  // Useful in cases where we are not sure in what state the system is at a particular moment, e.g. during cleanup
  async tryAction(action: () => Promise<void>, timeoutMs: number = 3000): Promise<boolean> {
    const originalTimeout = testConfig.actionTimeout;
    this.page.setDefaultTimeout(timeoutMs);
    try {
      await action();
      return true;
    } catch {
      return false;
    } finally {
      this.page.setDefaultTimeout(originalTimeout);
    }
  }

  private activeFilterEditButton(filterLabel: string): Locator {
    return this.page
      .locator('button[data-testid^="active-filter-"][data-testid$="-edit"]')
      .filter({ hasText: filterLabel });
  }

  private fleetViewTabsContainer(): Locator {
    return this.page.getByTestId(this.isMobile ? "fleet-view-tabs-mobile" : "fleet-view-tabs-desktop");
  }

  private fleetViewTabsTrigger(): Locator {
    return this.fleetViewTabsContainer().getByTestId("fleet-view-tabs-trigger");
  }

  private viewsEmptyStateNewButton(): Locator {
    return this.fleetViewTabsContainer().getByTestId("fleet-view-tabs-new-view-button");
  }

  private viewsPopover(): Locator {
    return this.page.getByTestId("fleet-view-tabs-views-popover");
  }

  private kebabButton(): Locator {
    return this.fleetViewTabsContainer().getByTestId("fleet-view-tabs-kebab");
  }

  private kebabPopover(): Locator {
    return this.page.getByTestId("fleet-view-tabs-kebab-popover");
  }

  private viewRow(viewName: string): Locator {
    return this.viewsPopover().locator('[data-testid^="fleet-view-row-"]').filter({ hasText: viewName });
  }

  private async openViewsPopover() {
    if (this.isMobile) {
      await this.dismissMobilePopoverSheet("fleet-view-tabs-views-popover");
    }

    await this.fleetViewTabsTrigger().click();
    await expect(this.viewsPopover()).toBeVisible();
  }

  private async openKebabPopover() {
    if (this.isMobile) {
      await this.dismissMobilePopoverSheet("fleet-view-tabs-kebab-popover");
    }

    await this.kebabButton().click();
    await expect(this.kebabPopover()).toBeVisible();
  }

  private async getVisibleAddFilterTrigger(): Promise<Locator> {
    const triggers = this.page.getByTestId("filter-nested-add-filter");
    let visibleIndex = -1;

    await expect
      .poll(
        async () => {
          const count = await triggers.count();

          for (let i = 0; i < count; i++) {
            const trigger = triggers.nth(i);
            if (await trigger.isVisible().catch(() => false)) {
              visibleIndex = i;
              return "visible";
            }
          }

          return "hidden";
        },
        {
          timeout: DEFAULT_TIMEOUT,
          message: "Expected a visible Add Filter trigger.",
        },
      )
      .toBe("visible");

    return triggers.nth(visibleIndex);
  }

  private async openVisibleAddFilter() {
    const trigger = await this.getVisibleAddFilterTrigger();
    await trigger.click();
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");
    await expect(popover).toBeVisible();
    return popover;
  }

  private async openNestedFilterSubmenu(popover: Locator, categoryKey: string) {
    await popover.getByTestId(`nested-dropdown-filter-row-${categoryKey}`).click();
    const desktopSubmenu = this.page.getByTestId(`nested-dropdown-filter-submenu-${categoryKey}`);
    const mobileBack = popover.getByTestId("nested-dropdown-filter-back");
    await expect(desktopSubmenu.or(mobileBack)).toBeVisible();

    if (await desktopSubmenu.isVisible().catch(() => false)) {
      return desktopSubmenu;
    }

    return popover;
  }

  private async dismissNestedAddFilterPopover() {
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");
    if (!(await popover.isVisible().catch(() => false))) {
      return;
    }

    if (this.isMobile) {
      await this.dismissMobilePopoverSheet("nested-dropdown-filter-popover");
      await expect(popover).toBeHidden();
      return;
    }

    const trigger = await this.getVisibleAddFilterTrigger();
    await trigger.click();
    await expect(popover).toBeHidden();
  }

  protected async dismissMobilePopoverSheet(popoverTestId: string) {
    const sheet = this.page.getByTestId(`${popoverTestId}-sheet`);

    if (!(await sheet.isVisible().catch(() => false))) {
      return;
    }

    try {
      await sheet.click({ position: { x: 1, y: 1 }, timeout: 1000 });
    } catch {
      if (!(await sheet.isVisible().catch(() => false))) {
        return;
      }

      await this.page.mouse.click(1, 1).catch(() => undefined);
    }

    if (await sheet.isVisible().catch(() => false)) {
      await this.page.keyboard.press("Escape").catch(() => undefined);
      await expect(sheet).toBeHidden();
    }
  }

  protected async clickOverflowAction(text: string) {
    const overflowTriggers = this.page.getByTestId("overflow-menu-trigger");
    const count = await overflowTriggers.count();

    for (let i = 0; i < count; i++) {
      const overflowTrigger = overflowTriggers.nth(i);
      if (!(await overflowTrigger.isVisible().catch(() => false))) {
        continue;
      }

      try {
        await overflowTrigger.click({ timeout: 1000 });
      } catch {
        continue;
      }

      const sheetContent = await this.findVisibleActionSheetContent(1000).catch(() => null);
      const action = sheetContent?.getByRole("button", { name: text, disabled: false, exact: true });
      if (action && (await action.isVisible().catch(() => false))) {
        await action.click();
        return true;
      }

      await this.dismissVisibleActionSheet();
    }

    return false;
  }

  protected async clickResponsiveTestId(testId: string, container?: Locator) {
    const root = container ?? this.page;
    const mobileButton = root.getByTestId(`${testId}-mobile`);

    if (this.isMobile && (await mobileButton.isVisible().catch(() => false))) {
      await mobileButton.click();
      return;
    }

    await root.getByTestId(testId).click();
  }

  private async findVisibleButton(text: string) {
    const buttons = this.page.getByRole("button", { name: text, disabled: false, exact: true });
    const count = await buttons.count();

    for (let i = 0; i < count; i++) {
      const button = buttons.nth(i);
      if (await button.isVisible().catch(() => false)) {
        return button;
      }
    }

    return null;
  }

  private async findVisibleActionSheetContent(timeout: number = DEFAULT_TIMEOUT) {
    const sheetContentTestIds = [
      "modal-overflow-sheet-content",
      "responsive-action-sheet-content",
      "action-sheet-content",
      "building-page-action-sheet-content",
      "list-header-action-sheet-content",
      "rack-slot-actions-sheet-content",
    ];

    await expect
      .poll(
        async () => {
          for (const testId of sheetContentTestIds) {
            if (
              await this.page
                .getByTestId(testId)
                .isVisible()
                .catch(() => false)
            ) {
              return testId;
            }
          }

          return "";
        },
        { timeout, message: "Expected a visible action sheet content area." },
      )
      .not.toBe("");

    for (const testId of sheetContentTestIds) {
      const content = this.page.getByTestId(testId);
      if (await content.isVisible().catch(() => false)) {
        return content;
      }
    }

    return null;
  }

  private async dismissVisibleActionSheet() {
    const sheetTestIds = [
      "modal-overflow-sheet",
      "responsive-action-sheet",
      "action-sheet",
      "building-page-action-sheet",
      "list-header-action-sheet",
      "rack-slot-actions-sheet",
    ];

    for (const testId of sheetTestIds) {
      const sheet = this.page.getByTestId(testId);
      if (await sheet.isVisible().catch(() => false)) {
        try {
          await sheet.click({ position: { x: 1, y: 1 }, timeout: 1000 });
        } catch {
          if (!(await sheet.isVisible().catch(() => false))) {
            return;
          }

          await this.page.mouse.click(1, 1).catch(() => undefined);
        }

        if (await sheet.isVisible().catch(() => false)) {
          await this.page.keyboard.press("Escape").catch(() => undefined);
          await expect(sheet).toBeHidden();
        }

        return;
      }
    }
  }

  private async waitForActiveFilterToClear(categoryKey: string) {
    await expect
      .poll(
        async () => ((await this.findVisibleTestIdLocator(`active-filter-${categoryKey}-edit`)) ? "visible" : "hidden"),
        {
          timeout: DEFAULT_TIMEOUT,
          message: `Expected active "${categoryKey}" filter chip to clear.`,
        },
      )
      .toBe("hidden");
  }

  private async findVisibleTestIdLocator(testId: string): Promise<Locator | null> {
    const matches = this.page.getByTestId(testId);
    const count = await matches.count();
    const visibleIndexes: number[] = [];

    for (let i = 0; i < count; i++) {
      const candidate = matches.nth(i);
      if (await candidate.isVisible().catch(() => false)) {
        visibleIndexes.push(i);
      }
    }

    if (visibleIndexes.length === 0) {
      return null;
    }

    if (visibleIndexes.length > 1) {
      throw new Error(`Expected a single visible locator for test id "${testId}", found ${visibleIndexes.length}.`);
    }

    return matches.nth(visibleIndexes[0]);
  }

  private async visibleTestIdLocator(testId: string): Promise<Locator> {
    await expect
      .poll(async () => ((await this.findVisibleTestIdLocator(testId)) ? "single" : "none"), {
        timeout: DEFAULT_TIMEOUT,
        message: `Expected a single visible locator for test id "${testId}".`,
      })
      .toBe("single");

    const match = await this.findVisibleTestIdLocator(testId);
    if (!match) {
      throw new Error(`Expected a visible locator for test id "${testId}".`);
    }

    return match;
  }

  private async visibleContainerTestIdLocator(container: Locator, testId: string): Promise<Locator> {
    const matches = container.getByTestId(testId);
    const count = await matches.count();
    const visibleIndexes: number[] = [];

    for (let i = 0; i < count; i++) {
      const candidate = matches.nth(i);
      if (await candidate.isVisible().catch(() => false)) {
        visibleIndexes.push(i);
      }
    }

    if (visibleIndexes.length !== 1) {
      throw new Error(`Expected a single visible locator for test id "${testId}" within the current filter container.`);
    }

    return matches.nth(visibleIndexes[0]);
  }
}
