import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class ServerLogsPage extends BasePage {
  async validateServerLogsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/server-logs/);
    await this.validateTitle("Server Logs");
  }

  async waitForLogRowCount(expectedCount: number) {
    await expect(this.page.getByTestId("log-row")).toHaveCount(expectedCount);
  }

  async validateLogRowVisible(text: string) {
    await expect(this.page.getByTestId("log-row").filter({ hasText: text })).toBeVisible();
  }

  async clickExport() {
    await this.page.getByRole("button", { name: "Export", exact: true }).click();
  }

  async validateFetchErrorCallout(message?: string) {
    const callout = this.page.getByTestId("server-logs-error");
    await expect(callout).toBeVisible();
    await expect(callout).toContainText("Couldn't load server logs");
    if (message) {
      await expect(callout).toContainText(message);
    }
  }

  async validateExportErrorCallout(message?: string) {
    const callout = this.page.getByTestId("server-logs-export-error");
    await expect(callout).toBeVisible();
    await expect(callout).toContainText("Couldn't export server logs");
    if (message) {
      await expect(callout).toContainText(message);
    }
  }
}
