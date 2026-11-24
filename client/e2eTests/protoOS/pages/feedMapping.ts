import { expect, type Page } from "@playwright/test";

export class FeedMappingPage {
  constructor(private page: Page) {}

  dialog() {
    return this.page.getByRole("dialog", { name: "Hashboard mapping", exact: true });
  }
  async review() {
    await this.page.getByRole("button", { name: "Refresh mapping", exact: true }).click();
  }
  async start() {
    await this.dialog().getByRole("button", { name: "Refresh mapping", exact: true }).click();
  }
  async progress() {
    await this.page.getByRole("button", { name: "View progress", exact: true }).click();
    await expect(this.dialog().getByTestId("mapping-progress-spinner")).toBeVisible();
  }
  async close() {
    await this.dialog().getByRole("button", { name: "Close", exact: true }).click();
  }
  poolConnectionWarning() {
    return this.page.getByText("This miner has lost connection to all mining pools.", { exact: true });
  }
  async dismiss() {
    await this.dialog().getByRole("button", { name: "Dismiss run result", exact: true }).click();
    await this.dialog().getByRole("button", { name: "Close", exact: true }).click();
  }
}
