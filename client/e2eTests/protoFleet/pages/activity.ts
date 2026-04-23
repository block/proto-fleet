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

  async selectUserFilter(optionLabel: string) {
    await this.selectDropdownFilter("Users", optionLabel);
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
