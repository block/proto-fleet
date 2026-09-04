import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";
import { ModalMinerSelectionList } from "./components/modalMinerSelectionList";

type FirmwareUploadMetadata = {
  manufacturer: string;
  model: string;
  firmwareVersion: string;
};

export class SettingsFirmwarePage extends BasePage {
  private readonly modalMinerList = new ModalMinerSelectionList(this.page.getByTestId("modal"));

  async validateFirmwarePageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/firmware/);
    await this.validateTitle("Firmware");
  }

  // --- Release channels ---

  async openFilesTab() {
    await this.page.getByRole("button", { name: "Files", exact: true }).click();
    await expect(this.page.getByRole("button", { name: "Upload firmware" })).toBeVisible();
  }

  async openReleaseChannelsTab() {
    await this.page.getByRole("button", { name: "Release channels", exact: true }).click();
    await this.validateTitle("Release channels");
    // The channels table renders only after loading finishes; helpers like
    // deleteChannelIfPresent would otherwise race and see no channels.
    await expect(this.page.getByText("Loading release channels...", { exact: true })).toBeHidden();
  }

  // The manage view for a channel, shown after drilling in via "Manage" or
  // right after creating it.
  channelView(channelName: string): Locator {
    return this.page.getByTestId(`release-channel-${channelName}`);
  }

  // The channel's row in the release channels table (a shared List row,
  // located through the name cell it contains).
  channelRow(channelName: string): Locator {
    return this.page.getByTestId("list-row").filter({ has: this.page.getByTestId(`channel-row-${channelName}`) });
  }

  // Opens the create form and names the channel; scope and behavior are set
  // with the helpers below before saveNewChannel.
  async startCreateChannel(channelName: string) {
    await this.clickButton("Create release channel");
    await expect(this.page.getByTestId("release-channel-new")).toBeVisible();
    await this.page.locator("#channel-name").fill(channelName);
  }

  async saveNewChannel(channelName: string) {
    const save = this.page.getByTestId("save-channel");
    await expect(save).toBeEnabled();
    await save.click();
    await this.validateTextInToast(`Created release channel ${channelName}`);
    await expect(this.channelView(channelName)).toBeVisible();
  }

  async saveChannelChanges() {
    const save = this.page.getByTestId("save-channel");
    await expect(save).toBeEnabled();
    await save.click();
    await this.validateTextInToast("Release channel saved");
  }

  // The save action is blocked because the scope overlaps another channel.
  async validateScopeConflict(otherChannelName: string) {
    await expect(this.page.getByTestId("scope-conflicts")).toContainText(otherChannelName);
    await expect(this.page.getByTestId("save-channel")).toBeDisabled();
  }

  // Opens the "Miners" selector of the Applies to section.
  async openScopeMiners() {
    await this.page
      .getByTestId("scope-editor")
      .getByRole("button", { name: /^Miners / })
      .click();
    await this.validateTitleInModal("Select miners");
    await this.modalMinerList.waitForListToLoad();
  }

  // Ticks the checkbox of `count` currently unchecked miners whose model
  // column matches. Already-selected members keep their state.
  async selectScopeMinersByModel(model: string, count: number) {
    const models = await this.modalMinerList.getVisibleCellTexts("type");
    const rows = this.page.getByTestId("modal").getByTestId("list-row");
    const indexes: number[] = [];
    for (let i = 0; i < models.length && indexes.length < count; i++) {
      if (models[i] !== model) {
        continue;
      }
      const checkbox = rows.nth(i).getByTestId("checkbox").locator("input").first();
      if (!(await checkbox.isChecked())) {
        indexes.push(i);
      }
    }
    expect(indexes, `found ${indexes.length} selectable ${model} miners, needed ${count}`).toHaveLength(count);
    await this.modalMinerList.selectRowsByIndex(indexes);
  }

  // Toggles one miner by display name (selects or deselects).
  async toggleScopeMinerByName(name: string) {
    await this.modalMinerList.selectRowByCellText("name", name);
  }

  async confirmScopeMinerSelection() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  // The Applies to preview resolved to this many miners.
  async validateScopeCovers(count: number) {
    await expect(this.page.getByTestId("scope-preview")).toContainText(
      `covers ${count} ${count === 1 ? "miner" : "miners"}`,
    );
  }

  // --- Update behavior controls ---

  async setMethod(label: "Single batch" | "Multiple batches" | "Pilot batch, then remaining") {
    await this.page.getByTestId("rollout-method").click();
    await this.page.getByRole("option", { name: label }).click();
  }

  async setPilotSize(size: number) {
    await this.page.locator("#pilot-size").fill(String(size));
  }

  async setBatchSize(size: number) {
    await this.page.locator("#batch-size").fill(String(size));
  }

  // The Switch's checkbox is visually hidden; its label toggles it.
  private async turnOnSwitch(inputId: string, label: string) {
    const input = this.page.locator(`#${inputId}`);
    if (!(await input.isChecked())) {
      await this.page.getByText(label, { exact: true }).click();
    }
    await expect(input).toBeChecked();
  }

  async enableReviewAfterEachBatch() {
    await this.turnOnSwitch("review-after-each-batch", "Review after each batch");
  }

  async enableAutoContinue({ maxHashrateDropPercent }: { maxHashrateDropPercent: number }) {
    await this.turnOnSwitch("auto-continue", "Auto-continue healthy batches");
    await this.page.locator("#max-hashrate-drop").fill(String(maxHashrateDropPercent));
    await this.page.locator("#max-efficiency-increase").fill("");
    await this.page.locator("#max-temp-increase").fill("");
    await this.page.locator("#max-errors").fill("");
    await this.page.locator("#stabilization-minutes").fill("0");
  }

  // Drills from the channels table into the channel's manage view.
  async manageChannel(channelName: string) {
    await this.channelRow(channelName).getByTestId(`manage-channel-${channelName}`).click();
    await expect(this.channelView(channelName)).toBeVisible();
  }

  // Returns from the manage view to the channels table.
  async backToChannels() {
    await this.page.getByTestId("back-to-channels").click();
    await expect(this.page.getByTestId("channels-table")).toBeVisible();
  }

  // Miner count shown in the channel's table row.
  async validateChannelMinerCount(channelName: string, count: number) {
    await expect(this.channelRow(channelName).getByTestId(`channel-miners-${channelName}`)).toHaveText(
      count.toLocaleString(),
    );
  }

  // Miner count in the manage view header.
  async validateChannelViewMinerCount(channelName: string, count: number) {
    const label = count === 1 ? "1 miner" : `${count} miners`;
    await expect(this.channelView(channelName).getByText(label, { exact: true }).first()).toBeVisible();
  }

  async expandChannel(channelName: string) {
    await this.page.getByTestId(`channel-toggle-${channelName}`).click();
  }

  // The expanded per-model row reports an ongoing update.
  async validateModelRowUpdating(channelName: string, model: string) {
    await expect(this.page.getByTestId(`model-status-${channelName}-${model}`)).toContainText("Updating");
  }

  // The channel row's status column reports this many active updates.
  async validateChannelActiveUpdates(channelName: string, count: number) {
    await expect(this.channelRow(channelName).getByTestId(`channel-status-${channelName}`)).toContainText(
      `${count} updating`,
    );
  }

  // The channel row's status column flags an update waiting on a human.
  async validateChannelNeedsAttention(channelName: string) {
    await expect(this.channelRow(channelName).getByTestId(`channel-status-${channelName}`)).toContainText(
      /needs? attention/,
    );
  }

  activeUpdateRow(channelName: string, model: string): Locator {
    return this.page.locator('[data-testid^="active-update-"]').filter({
      hasText: `${channelName}, ${model} firmware update`,
    });
  }

  async validateActiveUpdateRow(channelName: string, model: string) {
    await expect(this.activeUpdateRow(channelName, model)).toBeVisible();
  }

  // Opens the detail of an active update, whichever action label the banner
  // currently carries ("View update" or "Review update").
  async openActiveUpdate(channelName: string, model: string) {
    await this.activeUpdateRow(channelName, model)
      .getByRole("button", { name: /^(View|Review) update$/ })
      .click();
    await this.validateTitleInModal(`${channelName}, ${model} firmware update`);
    await expect(this.page.getByTestId("modal").getByText("Update status", { exact: true })).toBeVisible();
  }

  async closeUpdateDetail() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Close update details" }).click();
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  minersModal(): Locator {
    return this.page.getByTestId("modal");
  }

  // Opens the model group's miner table via its "View miners" button.
  async openModelMiners(channelName: string, model: string) {
    await this.channelView(channelName).getByTestId(`view-miners-${model}`).click();
    await this.validateTitleInModal(`${model} miners`);
    // Members are fetched when the modal opens; rows appear once loaded.
    await expect(this.minersModal().getByTestId("channel-miners-loading")).toBeHidden();
  }

  async closeModelMiners() {
    await this.minersModal().getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.minersModal()).toBeHidden();
  }

  // Display names of the model group's miners, in render order.
  async getChannelMinerNames(channelName: string, model: string): Promise<string[]> {
    await this.openModelMiners(channelName, model);
    const rows = this.minersModal().locator('[data-testid^="channel-miner-"]');
    const count = await rows.count();
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      names.push((await rows.nth(i).locator("td").first().innerText()).trim());
    }
    await this.closeModelMiners();
    return names;
  }

  // The miners modal lists exactly the model group's miners.
  async validateModelMinersCount(channelName: string, model: string, count: number) {
    await this.openModelMiners(channelName, model);
    await expect(this.minersModal().locator('[data-testid^="channel-miner-"]')).toHaveCount(count);
    await this.closeModelMiners();
  }

  // Number of the model group's miners currently reporting this version.
  async countModelMinersOnVersion(channelName: string, model: string, version: string): Promise<number> {
    await this.openModelMiners(channelName, model);
    const rows = this.minersModal().locator('[data-testid^="channel-miner-"]');
    const count = await rows.count();
    let matches = 0;
    for (let i = 0; i < count; i++) {
      if ((await rows.nth(i).innerText()).includes(version)) {
        matches += 1;
      }
    }
    await this.closeModelMiners();
    return matches;
  }

  async selectChannelFirmware(channelName: string, model: string, optionLabel: string | RegExp) {
    await this.channelView(channelName).getByTestId(`channel-firmware-select-${model}`).click();
    await this.page.getByRole("option", { name: optionLabel }).click();
  }

  // The firmware picker of the model group shows this version.
  async validateModelAssignedFirmware(channelName: string, model: string, version: string | RegExp) {
    await expect(this.channelView(channelName).getByTestId(`channel-firmware-select-${model}`)).toContainText(version);
  }

  private applyDialog(): Locator {
    return this.page.getByTestId("apply-firmware-dialog");
  }

  // Applies staged firmware changes through the confirmation dialog; the
  // update runs with the channel's saved behavior.
  async applyFirmwareChanges(channelName: string) {
    await this.channelView(channelName).getByTestId("apply-firmware-changes").click();
    await expect(this.applyDialog()).toBeVisible();
    await this.applyDialog().getByRole("button", { name: "Start update", exact: true }).click();
    await expect(this.applyDialog()).toBeHidden();
    await this.validateTextInToast("Firmware changes applied");
  }

  async validateChannelUpdateInProgress(channelName: string, version: string) {
    await expect(this.channelView(channelName).getByText(`Updating to ${version}`)).toBeVisible({
      timeout: DEFAULT_TIMEOUT,
    });
  }

  async validateChannelUpdatePill(channelName: string) {
    await expect(this.channelView(channelName).getByTestId("channel-update-pill")).toBeVisible();
  }

  // The model group sits at a review gate: the batch is on the new version
  // and the rest wait for the update to be continued.
  async waitForModelReviewNeeded(channelName: string, timeoutMs: number) {
    await expect(this.channelView(channelName).getByText("Review needed", { exact: true }).first()).toBeVisible({
      timeout: timeoutMs,
    });
  }

  private detailModal(): Locator {
    return this.page.getByTestId("modal");
  }

  async validateDetailHeadline(text: string | RegExp) {
    await expect(this.detailModal().getByTestId("rollout-status-headline")).toHaveText(text);
  }

  // The review evidence is on screen: verification lockups in the stats
  // grid and the telemetry strip with its error count.
  async validateEvidenceVisible() {
    const stats = this.detailModal().getByTestId("rollout-detail-stats");
    await expect(stats.getByTestId("evidence-online")).toContainText(/\d+ of \d+/);
    await expect(stats.getByTestId("evidence-hashing")).toContainText(/\d+ of \d+/);
    const evidence = this.detailModal().getByTestId("rollout-evidence");
    await expect(evidence).toBeVisible();
    await expect(evidence.getByTestId("evidence-hashrate")).toBeVisible();
    await expect(evidence.getByTestId("evidence-errors")).toBeVisible();
  }

  // Opens the miners drill-down from the detail's overflow menu and checks
  // it lists this many miners, then closes it.
  async validateDetailMinersCount(count: number) {
    await this.detailModal().getByTestId("view-rollout-more-actions-trigger").click();
    await this.page.getByTestId("view-rollout-view-miners-action").click();
    const miners = this.page.getByTestId("rollout-miners-modal");
    await expect(miners.getByTestId("list-row")).toHaveCount(count);
    await miners.getByRole("button", { name: "Done", exact: true }).click();
    await expect(miners).toBeHidden();
  }

  // Pause from the detail header and confirm the update reports paused;
  // then resume and confirm it no longer does.
  async pauseAndResumeFromDetail() {
    await this.detailModal().getByRole("button", { name: "Pause", exact: true }).click();
    await this.validateTextInToast("Paused");
    await this.validateDetailHeadline("Paused");
    await expect(this.detailModal().getByTestId("paused-banner")).toBeVisible();
    await this.detailModal().getByRole("button", { name: "Resume", exact: true }).click();
    await this.validateTextInToast("Resumed");
    await expect(this.detailModal().getByTestId("paused-banner")).toBeHidden();
  }

  // Releases the review gate from the detail header; the detail stays open
  // showing the next step, so close it explicitly afterwards.
  async continueFromDetail() {
    await this.detailModal().getByRole("button", { name: "Continue", exact: true }).click();
    await this.validateTextInToast("Continuing");
    await expect(this.detailModal().getByRole("button", { name: "Continue", exact: true })).toBeHidden();
    await this.closeUpdateDetail();
  }

  // Cancels the remaining update from the detail's overflow menu through the
  // confirmation dialog.
  async cancelRemainingFromDetail() {
    await this.detailModal().getByTestId("view-rollout-more-actions-trigger").click();
    await this.page.getByTestId("view-rollout-cancel-action").click();
    const dialog = this.page.getByTestId("cancel-rollout-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel remaining", exact: true }).click();
    await expect(dialog).toBeHidden();
    await this.validateTextInToast("Canceled");
    await this.closeUpdateDetail();
  }

  historyModal(): Locator {
    return this.page.getByTestId("modal");
  }

  async openChannelHistory(channelName: string) {
    await this.channelView(channelName).getByTestId("channel-history").click();
    await this.validateTitleInModal("Update history");
  }

  async closeChannelHistory() {
    await this.historyModal().getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.historyModal()).toBeHidden();
  }

  // A history entry for this version carries the given outcome label.
  async validateHistoryOutcome(channelName: string, version: string, outcome: string) {
    await this.openChannelHistory(channelName);
    await expect(
      this.historyModal().locator("tr").filter({ hasText: version }).filter({ hasText: outcome }).first(),
    ).toBeVisible({ timeout: DEFAULT_TIMEOUT });
    await this.closeChannelHistory();
  }

  // The first history action that restores this version. Rolling an entry
  // back restores what was assigned before it, so the action lives on the
  // entry that replaced the version, not on the version's own entry.
  private historyRollbackButton(version: string): Locator {
    return this.historyModal()
      .getByRole("button", { name: `Roll back to ${version}`, exact: true })
      .first();
  }

  // Some history entry offers to restore this version.
  async validateHistoryRollbackAvailable(channelName: string, version: string) {
    await this.openChannelHistory(channelName);
    await expect(this.historyRollbackButton(version)).toBeVisible();
    await this.closeChannelHistory();
  }

  // Restores a past version from the channel's history via the rollback
  // action of the entry that replaced it, and the confirmation dialog (which
  // the active-updates monitor above the tabs owns).
  async rollbackChannelFirmware(channelName: string, version: string) {
    await this.openChannelHistory(channelName);
    await this.historyRollbackButton(version).click();
    await expect(this.historyModal()).toBeHidden();
    const dialog = this.page.getByTestId("rollback-firmware-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Roll back", exact: true }).click();
    await expect(dialog).toBeHidden();
    await this.validateTextInToast("Rolling");
    // The rollback opens the new update's detail; close it to keep working
    // in the manage view.
    await this.closeUpdateDetail();
  }

  appRolloutPill(): Locator {
    return this.page.getByRole("button", { name: "View ongoing firmware updates" });
  }

  async validateAppRolloutPill() {
    await expect(this.appRolloutPill()).toBeVisible();
  }

  // The header pill leads with the attention item instead of plain progress.
  async validateAppRolloutPillNeedsAttention() {
    await expect(this.appRolloutPill()).toContainText(/needs? attention/);
  }

  // Opens the app-header pill popover and follows its link to the release
  // channels view.
  async followAppRolloutPillToChannels() {
    await this.appRolloutPill().click();
    await this.page.getByRole("link", { name: "View release channels", exact: true }).click();
    await this.validateTitle("Release channels");
    await expect(this.page.getByText("Loading release channels...", { exact: true })).toBeHidden();
  }

  // The update is done when every miner in the model group reports the
  // target version (checked in the live "View miners" modal), the progress
  // bar clears, and the update shows up as completed in the channel's history.
  async waitForChannelUpdateCompleted(channelName: string, model: string, version: string, timeoutMs: number) {
    const view = this.channelView(channelName);
    await this.openModelMiners(channelName, model);
    const minerRows = this.minersModal().locator('[data-testid^="channel-miner-"]');
    const rowCount = await minerRows.count();
    for (let i = 0; i < rowCount; i++) {
      await expect(minerRows.nth(i)).toContainText(version, { timeout: timeoutMs });
    }
    await this.closeModelMiners();
    // Status flips on the next enforcement tick after the miners report in.
    await expect(view.getByText(`Updating to ${version}`)).toBeHidden({ timeout: timeoutMs });
    await this.openChannelHistory(channelName);
    await expect(
      this.historyModal().locator("tr").filter({ hasText: "Completed" }).filter({ hasText: version }).first(),
    ).toBeVisible({ timeout: DEFAULT_TIMEOUT });
    await this.closeChannelHistory();
  }

  // Deletes the channel from its open manage view. Deleting returns to the
  // channels table, where the row must be gone.
  async deleteChannel(channelName: string) {
    await this.channelView(channelName).getByTestId("delete-channel").click();
    const dialog = this.page.getByTestId("delete-channel-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Delete channel", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(this.channelRow(channelName)).toBeHidden();
  }

  async deleteChannelIfPresent(channelName: string) {
    if (
      await this.channelRow(channelName)
        .isVisible()
        .catch(() => false)
    ) {
      await this.manageChannel(channelName);
      await this.deleteChannel(channelName);
    }
  }

  // --- Firmware files ---

  async clickUploadFirmware() {
    await this.clickButton("Upload firmware");
    await this.validateTitleInModal("Upload firmware");
  }

  async uploadFirmwareFile(
    fileName: string,
    fileContents: string,
    { manufacturer, model, firmwareVersion }: FirmwareUploadMetadata,
  ) {
    const modal = this.page.getByTestId("modal");
    await modal.getByLabel("Manufacturer").click();
    await this.page.getByRole("option", { name: manufacturer, exact: true }).click();
    await modal.getByLabel("Model").click();
    await this.page.getByRole("option", { name: model, exact: true }).click();
    await modal.getByLabel("Firmware version").fill(firmwareVersion);
    await this.page.getByTestId("firmware-file-input").setInputFiles({
      name: fileName,
      mimeType: "application/octet-stream",
      buffer: Buffer.from(fileContents),
    });
    await modal.getByRole("button", { name: "Upload", exact: true }).click();
    await expect(modal).toBeHidden();
  }

  async validateFirmwareFileVisible(fileName: string) {
    await expect(this.page.getByTestId("list-body").locator("tr").filter({ hasText: fileName })).toBeVisible();
  }

  async deleteFirmwareFileByName(fileName: string) {
    const row = this.page.getByTestId("list-body").locator("tr").filter({ hasText: fileName }).first();

    if (!(await row.isVisible().catch(() => false))) {
      return;
    }

    const directDeleteButton = row.getByRole("button", { name: "Delete", exact: true });
    if (await directDeleteButton.isVisible().catch(() => false)) {
      await directDeleteButton.click();
    } else {
      await row.getByTestId("overflow-menu-trigger").click();
      await this.page.getByRole("button", { name: "Delete", exact: true }).click();
    }

    const dialog = this.page.getByTestId("delete-firmware-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(row).toBeHidden();
  }

  async deleteAllFirmwareFilesIfAny() {
    const emptyState = this.page.getByText("No firmware files uploaded", { exact: true });
    const firmwareRows = this.page.getByTestId("list-body").locator("tr");
    const loadingState = this.page.getByText("Loading firmware files...", { exact: true });
    const deleteAllButton = this.page.getByRole("button", { name: "Delete all", exact: true }).first();

    if (await loadingState.isVisible().catch(() => false)) {
      await expect(loadingState).toBeHidden();
    }

    await expect(async () => {
      const emptyVisible = await emptyState.isVisible().catch(() => false);
      const hasRows = (await firmwareRows.count()) > 0;

      expect(emptyVisible || hasRows).toBeTruthy();
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });

    if (await emptyState.isVisible().catch(() => false)) {
      return;
    }

    await expect(deleteAllButton).toBeEnabled();
    await deleteAllButton.click();
    const deleteAllDialog = this.page.getByTestId("delete-all-firmware-dialog");
    await deleteAllDialog.getByRole("button", { name: "Delete all" }).click();
    await expect(deleteAllDialog).toBeHidden();
    await expect(deleteAllButton).toBeHidden();
    await expect(emptyState).toBeVisible();
  }
}
