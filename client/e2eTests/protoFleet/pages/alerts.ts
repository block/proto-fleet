import { expect, Locator, Page } from "@playwright/test";
import { BasePage } from "./base";
import { ModalMinerSelectionList } from "./components/modalMinerSelectionList";

/**
 * Alerts settings page (webhook + Slack delivery channels).
 *
 * The "Test" actions ask the server to have Grafana deliver a synthetic alert to
 * the destination, so the selectors here drive both the pre-save test (in the
 * Add destination modal) and the per-row test on a saved destination.
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
    await expect(this.page.getByRole("button", { name: "Add destination", exact: true })).toHaveCount(0);
  }

  async openAddChannelModal() {
    await this.page.getByRole("button", { name: "Add destination" }).click();
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
    await this.clickIn("Save destination", "modal");
    await this.validateModalIsClosed();
  }

  // Channels and rules render in the same list component; both row lookups and
  // the prefix-cleanup loop below are shared.
  private rowByText(text: string): Locator {
    return this.page.getByRole("row").filter({ hasText: text });
  }

  // Cleanup helper: the count-down assertion after each delete keeps the loop
  // stable while the list re-renders.
  private async deleteRowsByPrefix(prefix: string) {
    const rows = this.rowByText(prefix);
    for (let remaining = await rows.count(); remaining > 0; remaining--) {
      await rows.first().getByTestId("list-actions-trigger").click();
      await this.page.getByText("Delete", { exact: true }).click();
      await expect(rows).toHaveCount(remaining - 1);
    }
  }

  private channelRow(name: string): Locator {
    return this.rowByText(name);
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

  // Cleanup helper: remove every destination whose name carries the given test prefix.
  async deleteChannelsByPrefix(prefix: string) {
    await this.deleteRowsByPrefix(prefix);
  }

  // --- Rules: the full-screen create/edit alert view ---

  // The full-screen editor keeps its own testId; the scope pickers stack above
  // it as regular modals, so getByTestId("modal") targets only the picker.
  private get fullScreenEditor(): Locator {
    return this.page.getByTestId("full-screen-two-pane-modal");
  }

  private readonly minerScopeList = new ModalMinerSelectionList(this.page.getByTestId("modal"));

  private ruleRow(name: string): Locator {
    return this.rowByText(name);
  }

  async openCreateAlert() {
    await this.page.getByRole("button", { name: "Add rule" }).click();
    await expect(this.fullScreenEditor).toBeVisible();
    await expect(this.fullScreenEditor.getByText("Create an alert")).toBeVisible();
  }

  async fillRuleName(name: string) {
    await this.fullScreenEditor.locator("#rule-name").fill(name);
  }

  async validateScopePreview(text: string) {
    await expect(this.fullScreenEditor.getByText(text, { exact: true })).toBeVisible();
  }

  // The Apply-to buttons carry an aria-label of "<row> <value>", e.g. "Miners 1 miner".
  async validateMinersScopeButton(value: string) {
    await expect(this.fullScreenEditor.getByRole("button", { name: `Miners ${value}`, exact: true })).toBeVisible();
  }

  async openMinersScopePicker() {
    await this.fullScreenEditor.getByRole("button", { name: /^Miners / }).click();
    await this.validateTitleInModal("Select miners");
    await this.minerScopeList.waitForListToLoad();
  }

  async selectMinersInScopePicker(count: number) {
    const indexes = await this.minerScopeList.getSelectableRowIndexes(count);
    expect(indexes).toHaveLength(count);
    await this.minerScopeList.selectRowsByIndex(indexes);
    await this.saveMinerScopePicker();
  }

  async selectAllMinersInScopePicker() {
    // The footer "Select all" (not the header page checkbox) sets the picker's
    // allSelected flag, which the editor persists as a true org-wide scope.
    await this.page.getByTestId("modal").getByRole("button", { name: "Select all", exact: true }).click();
    await this.saveMinerScopePicker();
  }

  // An org-wide rule opens the picker with every miner selected; clear before
  // narrowing to an explicit subset.
  async deselectAllMinersInScopePicker() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Select none", exact: true }).click();
  }

  private async saveMinerScopePicker() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Done", exact: true }).click();
    await this.validateModalIsClosed();
  }

  async saveRule() {
    await this.fullScreenEditor.getByRole("button", { name: "Save", exact: true }).click();
    await expect(this.fullScreenEditor).toBeHidden();
  }

  async validateRuleScopeCell(name: string, scopeText: string) {
    await expect(this.ruleRow(name).getByText(scopeText, { exact: true })).toBeVisible();
  }

  async openEditRule(name: string) {
    await this.ruleRow(name).getByTestId("list-actions-trigger").click();
    await this.page.getByText("Edit", { exact: true }).click();
    await expect(this.fullScreenEditor).toBeVisible();
    await expect(this.fullScreenEditor.getByText("Edit alert")).toBeVisible();
  }

  // Cleanup helper: remove every user rule whose name carries the given test prefix.
  async deleteRulesByPrefix(prefix: string) {
    await this.deleteRowsByPrefix(prefix);
  }
}
