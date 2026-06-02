import { type Download, expect, type Locator } from "@playwright/test";
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
    await expect(this.latestActivityRow().getByTestId("activity-row-failed-indicator")).toBeVisible();
  }

  async validateLatestActivityNotMarkedFailed() {
    await expect(this.latestActivityRow().getByTestId("activity-row-failed-indicator")).toHaveCount(0);
  }

  async validateActivityDescriptionVisible(description: string) {
    await expect(this.activityRowByDescription(description)).toBeVisible();
  }

  async validateActivityRowVisible(description: string, scopeText: string) {
    await expect(this.activityRowByDescriptionAndScope(description, scopeText)).toBeVisible();
  }

  async validateActivityRowUser(description: string, scopeText: string, username: string) {
    await expect(this.activityRowByDescriptionAndScope(description, scopeText).getByTestId("user")).toHaveText(
      username,
    );
  }

  async validateActivityRowNotMarkedFailed(description: string, scopeText: string) {
    await expect(
      this.activityRowByDescriptionAndScope(description, scopeText).getByTestId("activity-row-failed-indicator"),
    ).toHaveCount(0);
  }

  async validateCompletedActivityRowVisible(description: string, scopeText: string) {
    await this.waitForCompletedActivityRow(description, scopeText);
    await expect(this.completedActivityRowByDescriptionAndScope(description, scopeText)).toBeVisible();
  }

  async validateCompletedActivityRowUser(description: string, scopeText: string, username: string) {
    await this.waitForCompletedActivityRow(description, scopeText);
    await expect(this.completedActivityRowByDescriptionAndScope(description, scopeText).getByTestId("user")).toHaveText(
      username,
    );
  }

  async validateCompletedActivityRowNotMarkedFailed(description: string, scopeText: string) {
    await this.waitForCompletedActivityRow(description, scopeText);
    await expect(
      this.completedActivityRowByDescriptionAndScope(description, scopeText).getByTestId(
        "activity-row-failed-indicator",
      ),
    ).toHaveCount(0);
  }

  async validateActivityDescriptionNotVisible(description: string) {
    await expect(this.activityRowByDescription(description)).toHaveCount(0);
  }

  async validateFilterPillVisible(label: string) {
    await expect(this.filterPillByLabel(label)).toBeVisible();
  }

  async validateFilterPillNotVisible(label: string) {
    await expect(this.filterPillByLabel(label)).toHaveCount(0);
  }

  async removeFilterPill(label: string) {
    await this.filterPillByLabel(label).click();
    await this.waitForActivityListToLoad();
  }

  async exportCsvAndWaitForDownload(): Promise<Download> {
    const downloadPromise = this.page.waitForEvent("download");
    await this.page.getByRole("button", { name: "Export CSV", exact: true }).click();
    return await downloadPromise;
  }

  async openLatestActivityDetails() {
    await this.latestActivityRow().click();
    await this.validateTitleInModal("Actions");
  }

  async openActivityDetails(description: string, scopeText: string) {
    await this.activityRowByDescriptionAndScope(description, scopeText).click();
    await this.validateTitleInModal("Actions");
  }

  async openCompletedActivityDetails(description: string, scopeText: string) {
    await this.waitForCompletedActivityRow(description, scopeText);
    await this.completedActivityRowByDescriptionAndScope(description, scopeText).click();
    await this.validateTitleInModal("Actions");
  }

  async closeActivityDetails() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Close dialog", exact: true }).click();
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  async validateActivityDetailResult(expectedResult: "Success" | "Failure" | "In progress") {
    await expect(this.page.getByTestId("activity-detail-result")).toContainText(expectedResult);
  }

  async validateActivityDetailSucceededCount(count: number) {
    await expect(this.page.getByTestId("activity-detail-succeeded")).toContainText(
      `${count} ${count === 1 ? "miner" : "miners"}`,
    );
  }

  async validateActivityDetailFailedCount(count: number) {
    await expect(this.page.getByTestId("activity-detail-failed")).toContainText(
      `${count} ${count === 1 ? "miner" : "miners"}`,
    );
  }

  async waitForCompletedActivityDetails(succeededCount: number, failedCount: number, resultRowCount: number) {
    const expectedState = JSON.stringify({
      succeeded: `${succeededCount} ${succeededCount === 1 ? "miner" : "miners"}`,
      failed: `${failedCount} ${failedCount === 1 ? "miner" : "miners"}`,
      resultRows: resultRowCount,
    });

    await expect
      .poll(
        async () =>
          JSON.stringify({
            succeeded: await this.page
              .getByTestId("activity-detail-succeeded")
              .textContent()
              .then((text) => text?.replace(/^Succeeded/, "")),
            failed: await this.page
              .getByTestId("activity-detail-failed")
              .textContent()
              .then((text) => text?.replace(/^Failed/, "")),
            resultRows: await this.page.getByTestId("activity-detail-device-result-row").count(),
          }),
        {
          timeout: DEFAULT_TIMEOUT,
          intervals: [100, DEFAULT_INTERVAL],
        },
      )
      .toBe(expectedState);
  }

  async validateActivityDetailMinerResultVisible(minerIdentifier: string, status: "Success" | "Failed") {
    const resultsTable = this.page.getByTestId("activity-detail-device-results-table");
    const row = resultsTable.getByTestId("activity-detail-device-result-row").filter({
      hasText: minerIdentifier,
    });
    await expect(row).toBeVisible();
    await expect(row).toContainText(status);
  }

  async validateActivityDetailError(text: string) {
    await expect(
      this.page.getByTestId("activity-detail-batch-error").or(this.page.getByTestId("activity-detail-error")),
    ).toContainText(text);
  }

  async getVisibleActivityRowCount() {
    return await this.page.getByTestId("list-row").count();
  }

  async validateAnyActivityRowsVisible() {
    await expect(this.page.getByTestId("list-row").first()).toBeVisible();
  }

  async validateLoadMoreVisible() {
    await expect(this.loadMoreButton()).toBeVisible();
  }

  async clickLoadMore(previousRowCount: number) {
    await this.loadMoreButton().click();
    await expect
      .poll(async () => await this.getVisibleActivityRowCount(), {
        timeout: DEFAULT_TIMEOUT,
        intervals: [100, DEFAULT_INTERVAL],
      })
      .toBeGreaterThan(previousRowCount);
  }

  private latestActivityRow(): Locator {
    return this.page.getByTestId("list-row").first();
  }

  private activityRowByDescription(description: string): Locator {
    return this.page.getByTestId("list-row").filter({
      has: this.page.getByTestId("type").getByText(description, { exact: false }),
    });
  }

  private activityRowByDescriptionAndScope(description: string, scopeText: string): Locator {
    return this.activityRowByDescription(description)
      .filter({
        has: this.page.getByTestId("scope").getByText(scopeText, { exact: true }),
      })
      .first();
  }

  private completedActivityRowByDescriptionAndScope(description: string, scopeText: string): Locator {
    return this.activityRowByDescription(description)
      .filter({
        has: this.page.getByTestId("scope").getByText(scopeText, { exact: true }),
      })
      .filter({ hasText: "succeeded" })
      .first();
  }

  private async waitForCompletedActivityRow(description: string, scopeText: string) {
    await expect
      .poll(async () => await this.completedActivityRowByDescriptionAndScope(description, scopeText).count(), {
        timeout: DEFAULT_TIMEOUT,
        intervals: [100, DEFAULT_INTERVAL],
      })
      .toBeGreaterThan(0);
  }

  private filterPillByLabel(label: string): Locator {
    return this.page.getByTestId("activity-filter-pills").getByRole("button", { name: label, exact: true });
  }

  private loadMoreButton(): Locator {
    return this.page.getByTestId("activity-load-more-button");
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
