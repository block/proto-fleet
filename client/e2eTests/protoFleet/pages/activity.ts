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
    await expect(this.filterPillsContainer().getByRole("button", { name: label, exact: true })).toBeVisible();
  }

  async validateFilterPillNotVisible(label: string) {
    await expect(this.filterPillsContainer().getByRole("button", { name: label, exact: true })).toHaveCount(0);
  }

  async removeFilterPill(label: string) {
    await this.filterPillsContainer().getByRole("button", { name: label, exact: true }).click();
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
    await expect(this.latestActivityRow().getByText("Failed", { exact: true })).toBeVisible();
  }

  async validateLatestActivityNotMarkedFailed() {
    await expect(this.latestActivityRow().getByText("Failed", { exact: true })).toHaveCount(0);
  }

  async validateActivityDescriptionVisible(description: string) {
    await expect(this.activityRowByDescription(description)).toBeVisible();
  }

  async openLatestActivityDetails() {
    await this.latestActivityRow().click();
  }

  async validateActivityDetailModalOpened() {
    await expect(this.activityDetailModal()).toBeVisible();
    await expect(this.activityDetailModal()).toContainText("Actions");
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
    await this.page.getByTestId(`filter-dropdown-${title}`).click();
    const popover = this.page.getByTestId("dropdown-filter-popover");
    await expect(popover).toBeVisible();
    await popover.getByText(optionLabel, { exact: true }).click();
    await popover.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(popover).toBeHidden();
    await this.waitForActivityListToLoad();
  }

  private filterPillsContainer(): Locator {
    return this.page.getByTestId("activity-filter-pills");
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
