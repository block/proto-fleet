import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class LogsPage extends BasePage {
  async validateLogsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/logs/);
    await expect(this.page.getByLabel("Search")).toBeVisible();
    await expect(this.page.getByRole("button", { name: "Export" })).toBeVisible();
  }

  async validateLogRowsVisible() {
    await expect(this.page.getByTestId("log-row").first()).toBeVisible();
  }

  async getLogRowCount() {
    return await this.page.getByTestId("log-row").count();
  }

  async getLogRowCountByType(logType: "error" | "warn") {
    return await this.page.locator(`[data-testid="log-row"][data-log-type="${logType}"]`).count();
  }

  async searchLogs(query: string) {
    const searchInput = this.page.getByLabel("Search");
    await searchInput.fill(query);
  }

  async clearSearch() {
    const searchInput = this.page.getByLabel("Search");
    await searchInput.focus();
    await searchInput.press("Escape");
    await expect(searchInput).toHaveValue("");
  }

  async clickErrorFilter() {
    await this.page.getByTestId("logs-error-filter").click();
  }

  async clickWarningFilter() {
    await this.page.getByTestId("logs-warning-filter").click();
  }

  async validateOnlyLogTypeVisible(logType: "error" | "warn") {
    const rows = this.page.getByTestId("log-row");
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      await expect(rows.nth(i)).toHaveAttribute("data-log-type", logType);
    }
  }

  async validateNoResultsState(message: string) {
    await expect(this.page.getByTestId("logs-empty-state")).toBeVisible();
    await expect(this.page.getByTestId("logs-empty-state")).toHaveText(message);
  }

  async getSearchableSubstringFromFirstRow() {
    const firstRowText = (await this.page.getByTestId("log-row").first().textContent()) ?? "";
    const normalizedText = firstRowText
      .replace(/^\s*\d+\s*/, "")
      .replace(/^\[\d{2}:\d{2}:\d{2}\]\s*/, "")
      .trim();

    const match = normalizedText.match(/[A-Za-z0-9][A-Za-z0-9 .:_/-]{4,}/);
    const query = match?.[0]?.trim().slice(0, 12) ?? normalizedText.slice(0, 12).trim();

    expect(query, "Expected a searchable log substring from the first row").not.toBe("");
    return query;
  }

  async waitForLogsListToBeReady() {
    await expect(this.page.getByTestId("log-row").first()).toBeVisible();
  }
}
