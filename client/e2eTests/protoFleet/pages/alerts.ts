import { expect, Locator, Page } from "@playwright/test";
import { BasePage } from "./base";

/**
 * Alerts settings page (webhook + Slack delivery channels).
 *
 * The "Test" actions ask the server to have Grafana deliver a synthetic alert to
 * the destination, so the selectors here drive both the pre-save test (in the
 * Add channel modal) and the per-row test on a saved channel.
 */
export class AlertsPage extends BasePage {
  constructor(page: Page, isMobile: boolean = false) {
    super(page, isMobile);
  }

  async validateAlertsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/alerts/);
    await this.validateTitle("Alerts");
  }

  async validateAddChannelHidden() {
    await expect(this.page.getByRole("button", { name: "Add channel", exact: true })).toHaveCount(0);
  }

  async openAddChannelModal() {
    await this.page.getByRole("button", { name: "Add channel" }).click();
    await this.validateModalIsOpen();
  }

  async fillWebhookChannel(name: string, url: string) {
    await this.page.locator("#channel-name").fill(name);
    await this.page.locator("#channel-webhook-url").fill(url);
  }

  async sendTestFromModal() {
    await this.clickIn("Send test", "modal");
  }

  async saveChannel() {
    await this.clickIn("Save channel", "modal");
    await this.validateModalIsClosed();
  }

  private channelRow(name: string): Locator {
    return this.page.getByRole("row").filter({ hasText: name });
  }

  async validateChannelListed(name: string) {
    await expect(this.channelRow(name)).toBeVisible();
  }

  async validateChannelStatus(name: string, status: string) {
    await expect(this.channelRow(name).getByText(status, { exact: true })).toBeVisible();
  }

  private async openRowActions(name: string) {
    await this.channelRow(name).getByTestId("list-actions-trigger").click();
  }

  async testSavedChannel(name: string) {
    await this.openRowActions(name);
    await this.page.getByText("Test", { exact: true }).click();
  }

  async deleteChannel(name: string) {
    await this.openRowActions(name);
    await this.page.getByText("Delete", { exact: true }).click();
    await expect(this.channelRow(name)).toBeHidden();
  }

  // Cleanup helper: remove every channel whose name carries the given test prefix.
  async deleteChannelsByPrefix(prefix: string) {
    const rows = this.channelRow(prefix);
    for (let remaining = await rows.count(); remaining > 0; remaining--) {
      await rows.first().getByTestId("list-actions-trigger").click();
      await this.page.getByText("Delete", { exact: true }).click();
      await expect(rows).toHaveCount(remaining - 1);
    }
  }
}
