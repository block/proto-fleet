import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class SettingsNodesPage extends BasePage {
  private nodeRow(nodeName: string) {
    return this.page
      .getByTestId("list-body")
      .getByTestId("list-row")
      .filter({
        has: this.page.getByTestId("name").getByText(nodeName, { exact: true }),
      });
  }

  async validateNodesPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/nodes/);
    await this.validateTitle("Nodes");
  }

  async waitForNodesListToLoad() {
    await this.validateNodesPageOpened();
    const rows = this.page.getByTestId("list-body").getByTestId("list-row");
    const emptyState = this.page.getByText("No nodes yet", { exact: true });

    await expect(this.page.getByText("Loading nodes...")).toBeHidden();

    await expect(async () => {
      if (await emptyState.isVisible().catch(() => false)) {
        return;
      }

      const rowCount = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies the mocked/live row count has settled above zero
      expect(rowCount).toBeGreaterThan(0);
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));

      if (await emptyState.isVisible().catch(() => false)) {
        return;
      }

      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally compares two sampled counts to confirm the list has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async validateNodeVisible(nodeName: string) {
    await expect(this.nodeRow(nodeName)).toBeVisible();
  }

  async validateEnrollNodeVisible() {
    await expect(this.page.getByRole("button", { name: "Enroll node", exact: true })).toBeVisible();
  }

  async validateEnrollNodeHidden() {
    await expect(this.page.getByRole("button", { name: "Enroll node", exact: true })).toHaveCount(0);
  }

  async clickEnrollNode() {
    await this.clickButton("Enroll node");
  }

  async clickNodeActionsMenu(nodeName: string) {
    await this.nodeRow(nodeName).getByTestId("list-actions-trigger").click();
  }

  async clickNodeAction(actionName: "Confirm enrollment" | "Revoke") {
    await this.clickButton(actionName);
  }

  async validateNodeActionVisible(actionName: "Confirm enrollment" | "Revoke") {
    await expect(this.page.getByRole("button", { name: actionName, exact: true })).toBeVisible();
  }

  async validateNodeActionHidden(actionName: "Confirm enrollment" | "Revoke") {
    await expect(this.page.getByRole("button", { name: actionName, exact: true })).toHaveCount(0);
  }

  async validateEnrollNodeModalOpened() {
    const modal = this.page.getByTestId("modal");
    await expect(modal).toBeVisible();
    await expect(modal).toContainText("Enroll a node");
    await expect(modal).toContainText("1. On the host you want to enroll, run:");
  }

  async validateConfirmNodeModalOpened(nodeName: string) {
    const modal = this.page.getByTestId("modal");
    await expect(modal).toBeVisible();
    await expect(modal).toContainText("Confirm the node");
    await expect(modal).toContainText(nodeName);
    await expect(modal.getByRole("button", { name: "Confirm node", exact: true })).toBeVisible();
  }
}
