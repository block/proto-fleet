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
    await expect(this.page.getByRole("button", { name: "Set up a node", exact: true })).toBeVisible();
  }

  async validateEnrollNodeHidden() {
    await expect(this.page.getByRole("button", { name: "Set up a node", exact: true })).toHaveCount(0);
  }

  async clickEnrollNode() {
    await this.clickButton("Set up a node");
  }

  async clickConfirmEnrollment(nodeName: string) {
    await this.nodeRow(nodeName).getByRole("button", { name: "Confirm enrollment", exact: true }).click();
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
    await expect(modal).toContainText("Set up a node");
    await expect(modal.getByRole("checkbox", { name: "Enroll as a standby" })).not.toBeChecked();
    await expect(modal.getByRole("button", { name: "Copy command" })).toBeVisible();
    await expect(modal.getByRole("button", { name: "Generate code", exact: true })).toBeEnabled();
  }

  async chooseStandbyFor(nodeName: string, excludedNodeName: string) {
    const modal = this.page.getByTestId("modal");
    await modal.getByRole("checkbox", { name: "Enroll as a standby" }).check();
    await expect(modal.getByRole("button", { name: "Generate code", exact: true })).toBeDisabled();
    await modal.getByRole("button", { name: "Standby for", exact: true }).click();
    await expect(this.page.getByRole("option", { name: excludedNodeName, exact: true })).toHaveCount(0);
    await this.page.getByRole("option", { name: nodeName, exact: true }).click();
  }

  async generateEnrollmentCode() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Generate code", exact: true }).click();
  }

  async validateEnrollmentCode(code: string) {
    const modal = this.page.getByTestId("modal");
    await expect(modal.getByText(code, { exact: true })).toBeVisible();
    await expect(modal.getByRole("checkbox", { name: "Enroll as a standby" })).toBeDisabled();
    await expect(modal).toContainText("1. Run this command on the machine:");
  }

  async validateInstallationPrerequisite() {
    const modal = this.page.getByTestId("modal");
    const instructions = modal.getByRole("link", { name: "Installation instructions" });
    const command = modal.getByRole("button", { name: "Copy command" });
    await expect(modal).toContainText("Linux machine that can reach your miners and this Fleet server");
    await expect(instructions).toHaveAttribute("href", "https://docs.proto.xyz/fleet/fleet-node");
    await expect(command).toBeVisible();
    const instructionsBox = await instructions.boundingBox();
    const commandBox = await command.boundingBox();
    expect(instructionsBox).not.toBeNull();
    expect(commandBox).not.toBeNull();
    expect(instructionsBox!.y + instructionsBox!.height).toBeLessThan(commandBox!.y);
  }

  async confirmNode() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Confirm node", exact: true }).click();
  }

  async validateStandbyRelationship(nodeName: string) {
    await expect(this.page.getByTestId("modal")).toContainText(`Standby for ${nodeName}`);
  }

  async finishEnrollment(apiKey: string) {
    await expect(this.page.getByText(apiKey, { exact: true })).toBeVisible();
    await this.clickButton("Done");
    await expect(this.page.getByText(apiKey, { exact: true })).toHaveCount(0);
  }

  async validateConfirmNodeModalOpened(nodeName: string) {
    const modal = this.page.getByTestId("modal");
    await expect(modal).toBeVisible();
    await expect(modal).toContainText("Confirm the node");
    await expect(modal).toContainText(nodeName);
    await expect(modal.getByRole("button", { name: "Confirm node", exact: true })).toBeVisible();
  }

  async validateNodeRowText(nodeName: string, text: string) {
    await expect(this.nodeRow(nodeName)).toContainText(text);
  }

  async openNodeDetails(nodeName: string) {
    await this.nodeRow(nodeName).getByTestId("name").click();
    await expect(this.page.getByTestId("node-details-modal")).toBeVisible();
  }

  private haDetailValue(label: string) {
    return this.page
      .getByTestId("node-details-modal")
      .getByText(label, { exact: true })
      .locator("xpath=following-sibling::dd");
  }

  async validateNodeDetailsHa(role: string, peerLabel: string, peerName: string) {
    await expect(this.haDetailValue("Role")).toHaveText(role);
    await expect(this.haDetailValue(peerLabel)).toHaveText(peerName);
  }

  async validateNodeDetailsHaStatus(reason: string) {
    await expect(this.haDetailValue("HA status")).toHaveText(reason);
  }

  async validateFindMinersDisabledForStandby() {
    const details = this.page.getByTestId("node-details-modal");
    await expect(details.getByRole("button", { name: "Find miners", exact: true })).toBeDisabled();
    await expect(details).toContainText("Standby nodes can't find miners.");
  }

  async validateRevokeDialogText(nodeName: string, text: string) {
    const dialog = this.page.getByRole("dialog").filter({ hasText: "Revoke node?" });
    await expect(dialog).toContainText(`revoke "${nodeName}"`);
    await expect(dialog).toContainText(text);
  }
}
