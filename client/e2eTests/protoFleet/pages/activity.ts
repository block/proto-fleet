import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class ActivityPage extends BasePage {
  async validateActivityPageOpened() {
    await expect(this.page).toHaveURL(/.*\/activity/);
    await this.validateTitle("Activity");
  }

  async waitForActivityListToLoad() {
    await this.validateActivityPageOpened();
    await expect(async () => {
      const initialState = await this.getVisibleActivityListState();
      expect(initialState).not.toBe("loading");

      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));

      const settledState = await this.getVisibleActivityListState();
      expect(settledState).toBe(initialState);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [100, DEFAULT_INTERVAL] });
  }

  async searchActivity(searchText: string) {
    const input = this.page.locator("#activity-search");
    await input.fill(searchText);
    await this.waitForActivityListToLoad();
  }

  async clearSearchWithEscape() {
    const input = this.page.locator("#activity-search");
    await input.press("Escape");
    await this.waitForActivityListToLoad();
  }

  async selectTypeFilter(optionLabel: string) {
    await this.selectDropdownFilter("Type", optionLabel);
  }

  async selectScopeFilter(optionLabel: string) {
    await this.selectDropdownFilter("Scope", optionLabel);
  }

  async selectUserFilter(optionLabel: string) {
    await this.selectDropdownFilter("Users", optionLabel);
  }

  async validateFilterPillVisible(label: string) {
    await expect(this.activeFilterChipByLabel(label)).toBeVisible();
  }

  async validateFilterPillNotVisible(label: string) {
    await expect(this.activeFilterChipByLabel(label)).toHaveCount(0);
  }

  async removeFilterPill(label: string) {
    await this.activeFilterChipByLabel(label).locator('[data-testid$="-clear"]').click();
    await this.waitForActivityListToLoad();
  }

  async validateNoResultsVisible() {
    await expect(this.page.getByText("No results", { exact: true })).toBeVisible();
    await expect(this.page.getByTestId("clear-all-filters-button")).toBeVisible();
  }

  async clearAllFilters() {
    await this.page.getByTestId("clear-all-filters-button").click();
    await this.waitForActivityListToLoad();
  }

  async validateSearchInputValue(expectedValue: string) {
    await expect(this.page.locator("#activity-search")).toHaveValue(expectedValue);
  }

  async validateLatestActivityDescription(description: string) {
    await expect(this.latestActivityRow()).toContainText(description);
  }

  async validateLatestActivityUser(username: string) {
    await expect(this.latestActivityRow().getByTestId("user")).toHaveText(username);
  }

  async validateLatestActivityScope(scopeText: string) {
    await expect(this.latestActivityRow().getByTestId("scope")).toContainText(scopeText);
  }

  async validateLatestActivityMarkedFailed() {
    await expect(this.latestActivityRow().getByText("Couldn't complete", { exact: true })).toHaveCount(1);
  }

  async validateLatestActivityNotMarkedFailed() {
    await expect(this.latestActivityRow().getByText("Couldn't complete", { exact: true })).toHaveCount(0);
  }

  async validateActivityDescriptionVisible(description: string) {
    await expect(this.activityRowByDescription(description)).toBeVisible();
  }

  async validateActivityDescriptionMarkedFailed(description: string) {
    await expect(
      this.activityRowByDescription(description).getByText("Couldn't complete", { exact: true }),
    ).toHaveCount(1);
  }

  async openLatestActivityDetails() {
    await this.latestActivityRow().click();
  }

  async validateActivityDetailModalOpened() {
    await expect(this.activityDetailModal()).toBeVisible();
  }

  async validateActivityDetailContainsText(text: string) {
    await expect(this.activityDetailModal()).toContainText(text);
  }

  async validateActivityDetailDeviceResultsRowCount(expectedCount: number) {
    await expect(this.activityDetailModal().locator("tbody tr")).toHaveCount(expectedCount);
  }

  async dismissActivityDetailModal() {
    await this.activityDetailModal().getByTestId("header-icon-button").click();
    await expect(this.activityDetailModal()).toBeHidden();
  }

  async exportCsv() {
    const downloadPromise = this.page.waitForEvent("download");
    await this.page.getByRole("button", { name: "Export CSV", exact: true }).click();
    return await downloadPromise;
  }

  private latestActivityRow(): Locator {
    return this.page.getByTestId("list-row").first();
  }

  private activityRowByDescription(description: string): Locator {
    return this.page.getByTestId("list-row").filter({
      has: this.page.getByTestId("type").getByText(description, { exact: false }),
    });
  }

  private async selectDropdownFilter(title: string, optionLabel: string) {
    const filterKey = title === "Users" ? "users" : title.toLowerCase();
    const trigger = this.page.getByTestId("filter-nested-add-filter");
    const popover = this.page.getByTestId("nested-dropdown-filter-popover");

    if (await popover.isVisible().catch(() => false)) {
      await this.page.mouse.click(1, 1);
      await expect(popover).toBeHidden();
    }

    await trigger.click();
    await expect(popover).toBeVisible();

    await popover.getByTestId(`nested-dropdown-filter-row-${filterKey}`).click();

    if (this.isMobile) {
      await popover.getByText(optionLabel, { exact: true }).click();
    } else {
      const submenu = this.page.getByTestId(`nested-dropdown-filter-submenu-${filterKey}`);
      await expect(submenu).toBeVisible();
      await submenu.getByText(optionLabel, { exact: true }).click();
    }

    await this.page.mouse.click(1, 1);
    await expect(popover).toBeHidden();
    await this.waitForActivityListToLoad();
  }

  private activeFilterChipByLabel(label: string): Locator {
    return this.page
      .locator('[data-testid^="active-filter-"]:not([data-testid$="-edit"]):not([data-testid$="-clear"])')
      .filter({ has: this.page.getByRole("button", { name: label, exact: true }) });
  }

  private activityDetailModal(): Locator {
    return this.page.getByTestId("modal");
  }

  private async getVisibleActivityListState(): Promise<string> {
    const rows = this.page.getByTestId("list-row");
    const emptyState = this.page.getByText("No activity to display.");
    const noResults = this.page.getByText("No results", { exact: true });

    if (await noResults.isVisible().catch(() => false)) {
      return "no-results";
    }

    if (await emptyState.isVisible().catch(() => false)) {
      return "empty";
    }

    const rowCount = await rows.count();
    if (
      rowCount > 0 &&
      (await rows
        .first()
        .isVisible()
        .catch(() => false))
    ) {
      return `rows:${rowCount}`;
    }

    return "loading";
  }
}
