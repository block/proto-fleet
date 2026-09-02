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

  async openFilesTab() {
    await this.page.getByRole("button", { name: "Files", exact: true }).click();
    await expect(this.page.getByRole("button", { name: "Upload firmware" })).toBeVisible();
  }

  async openRolloutLanesTab() {
    await this.page.getByRole("button", { name: "Release channels", exact: true }).click();
    await this.validateTitle("Release channels");
    // The channels table renders only after loading finishes; helpers like
    // deleteLaneIfPresent would otherwise race and see no channels.
    await expect(this.page.getByText("Loading release channels...", { exact: true })).toBeHidden();
  }

  // Management card for a channel, shown after drilling in via "Manage".
  laneCard(laneName: string): Locator {
    return this.page.getByTestId(`rollout-lane-${laneName}`);
  }

  // The channel's row in the release channels table.
  channelRow(laneName: string): Locator {
    return this.page.getByTestId(`channel-row-${laneName}`);
  }

  async createLane(laneName: string) {
    await this.clickButton("Create release channel");
    const dialog = this.page.getByTestId("create-lane-dialog");
    await dialog.getByLabel("Channel name").fill(laneName);
    await dialog.getByRole("button", { name: "Create channel", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(this.channelRow(laneName)).toBeVisible();
  }

  // Drills from the channels table into the channel's management card.
  async manageChannel(laneName: string) {
    await this.channelRow(laneName).getByTestId(`manage-channel-${laneName}`).click();
    await expect(this.laneCard(laneName)).toBeVisible();
  }

  // Returns from the management card to the channels table.
  async backToChannels() {
    await this.page.getByTestId("back-to-channels").click();
    await expect(this.page.getByTestId("channels-table")).toBeVisible();
  }

  // Miner count shown in the channel's table row.
  async validateChannelMinerCount(laneName: string, count: number) {
    await expect(this.channelRow(laneName).getByTestId(`channel-miners-${laneName}`)).toHaveText(
      count.toLocaleString(),
    );
  }

  async expandChannel(laneName: string) {
    await this.page.getByTestId(`channel-toggle-${laneName}`).click();
  }

  // The expanded per-model row reports an ongoing update.
  async validateModelRowUpdating(laneName: string, model: string) {
    await expect(this.page.getByTestId(`model-row-${laneName}-${model}`)).toContainText("Updating,");
  }

  // The channel row's status column reports this many active updates.
  async validateChannelActiveUpdates(laneName: string, count: number) {
    await expect(this.channelRow(laneName).getByTestId(`channel-status-${laneName}`)).toHaveText(`${count} active`);
  }

  activeUpdateRow(laneName: string, model: string): Locator {
    return this.page.locator('[data-testid^="active-update-"]').filter({
      hasText: `${laneName}, ${model} firmware update`,
    });
  }

  async validateActiveUpdateRow(laneName: string, model: string) {
    await expect(this.activeUpdateRow(laneName, model)).toBeVisible();
  }

  // Opens the update detail modal from the active updates section.
  async openUpdateDetail(laneName: string, model: string) {
    await this.activeUpdateRow(laneName, model).getByRole("button", { name: "View update", exact: true }).click();
    await this.validateTitleInModal(`${laneName}, ${model} firmware update`);
    await expect(this.page.getByTestId("modal").getByText("Update status", { exact: true })).toBeVisible();
  }

  async closeUpdateDetail() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  async openManageLaneMiners(laneName: string) {
    await this.laneCard(laneName).getByRole("button", { name: "Manage miners", exact: true }).click();
    await this.validateTitleInModal("Select miners");
    await this.modalMinerList.waitForListToLoad();
  }

  // Ticks the checkbox of `count` currently unchecked miners whose model
  // column matches. Already-selected members keep their state.
  async selectLaneMinersByModel(model: string, count: number) {
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

  async selectLaneMinerByName(name: string) {
    await this.modalMinerList.selectRowByCellText("name", name);
  }

  async confirmLaneMinerSelection() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  minersModal(): Locator {
    return this.page.getByTestId("modal");
  }

  // Opens the model group's miner table via its "View miners" button.
  async openModelMiners(laneName: string, model: string) {
    await this.laneCard(laneName).getByTestId(`view-miners-${model}`).click();
    await this.validateTitleInModal(`${model} miners`);
  }

  async closeModelMiners() {
    await this.minersModal().getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.minersModal()).toBeHidden();
  }

  // Display names of the model group's miners, in render order.
  async getLaneMinerNames(laneName: string, model: string): Promise<string[]> {
    await this.openModelMiners(laneName, model);
    const rows = this.minersModal().locator('[data-testid^="lane-miner-"]');
    const count = await rows.count();
    const names: string[] = [];
    for (let i = 0; i < count; i++) {
      names.push((await rows.nth(i).locator("td").first().innerText()).trim());
    }
    await this.closeModelMiners();
    return names;
  }

  // The miners modal lists exactly the model group's miners.
  async validateModelMinersCount(laneName: string, model: string, count: number) {
    await this.openModelMiners(laneName, model);
    await expect(this.minersModal().locator('[data-testid^="lane-miner-"]')).toHaveCount(count);
    await this.closeModelMiners();
  }

  async validateLaneMinerCount(laneName: string, count: number) {
    const label = count === 1 ? "1 miner" : `${count} miners`;
    await expect(this.laneCard(laneName).getByText(label, { exact: true }).first()).toBeVisible();
  }

  async selectLaneFirmware(laneName: string, model: string, optionLabel: string | RegExp) {
    await this.laneCard(laneName).getByTestId(`lane-firmware-select-${model}`).click();
    await this.page.getByRole("option", { name: optionLabel }).click();
  }

  private applyDialog(): Locator {
    return this.page.getByTestId("apply-firmware-dialog");
  }

  // Applies staged firmware changes with the default method (all at once)
  // through the apply confirmation dialog.
  async applyLaneFirmwareChanges(laneName: string) {
    await this.laneCard(laneName).getByRole("button", { name: "Apply changes", exact: true }).click();
    await expect(this.applyDialog()).toBeVisible();
    await this.applyDialog().getByRole("button", { name: "Start rollout", exact: true }).click();
    await expect(this.applyDialog()).toBeHidden();
    await this.validateTextInToast("Firmware changes applied");
  }

  // Applies staged firmware changes as a pilot rollout: only `pilotSize`
  // miners per model update until the pilot is reviewed and continued.
  async applyLaneFirmwareChangesWithPilot(laneName: string, pilotSize: number) {
    await this.laneCard(laneName).getByRole("button", { name: "Apply changes", exact: true }).click();
    await expect(this.applyDialog()).toBeVisible();
    await this.applyDialog().getByTestId("apply-method-pilot").click();
    await this.applyDialog().getByLabel("Pilot size (miners per model)").fill(String(pilotSize));
    await this.applyDialog().getByRole("button", { name: "Start rollout", exact: true }).click();
    await expect(this.applyDialog()).toBeHidden();
    await this.validateTextInToast("Firmware changes applied");
  }

  // Applies staged firmware changes in fixed batches that advance on their
  // own once the evidence thresholds hold (no stabilization wait).
  async applyLaneFirmwareChangesWithAutoBatches(laneName: string, batchSize: number, maxHashrateDropPercent: number) {
    await this.laneCard(laneName).getByRole("button", { name: "Apply changes", exact: true }).click();
    await expect(this.applyDialog()).toBeVisible();
    await this.applyDialog().getByTestId("apply-method-batches").click();
    await this.applyDialog().getByLabel("Batch size (miners per batch)").fill(String(batchSize));
    await this.applyDialog().getByTestId("apply-auto-advance").click();
    await this.applyDialog().getByLabel("Max hashrate drop (%)").fill(String(maxHashrateDropPercent));
    await this.applyDialog().getByLabel("Stabilization (minutes)").fill("0");
    await this.applyDialog().getByRole("button", { name: "Start rollout", exact: true }).click();
    await expect(this.applyDialog()).toBeHidden();
    await this.validateTextInToast("Firmware changes applied");
  }

  // The model group sits at a review gate: the batch is on the new version
  // and the rest wait for the rollout to be continued.
  async waitForModelReviewNeeded(laneName: string, timeoutMs: number) {
    await expect(
      this.laneCard(laneName)
        .getByText(/complete — review needed/)
        .first(),
    ).toBeVisible({
      timeout: timeoutMs,
    });
  }

  // The header pill leads with the review instead of plain progress.
  async validateAppRolloutPillNeedsReview() {
    await expect(this.appRolloutPill()).toContainText(/needs? review/);
  }

  // Opens the detail modal of an active update, whichever action label the
  // row currently carries ("View update" or "Review update").
  async openActiveUpdate(laneName: string, model: string) {
    await this.activeUpdateRow(laneName, model)
      .getByRole("button", { name: /^(View|Review) update$/ })
      .click();
    await this.validateTitleInModal(`${laneName}, ${model} firmware update`);
  }

  private detailModal(): Locator {
    return this.page.getByTestId("modal");
  }

  async validateDetailHeadline(text: string | RegExp) {
    await expect(this.detailModal().getByTestId("rollout-status-headline")).toHaveText(text);
  }

  // The review evidence block is on screen with its four figures.
  async validateEvidenceVisible() {
    const evidence = this.detailModal().getByTestId("rollout-evidence");
    await expect(evidence).toBeVisible();
    await expect(evidence.getByTestId("evidence-online")).toContainText(/\d+ of \d+/);
    await expect(evidence.getByTestId("evidence-hashing")).toContainText(/\d+ of \d+/);
    await expect(evidence.getByTestId("evidence-errors")).toBeVisible();
  }

  // Pause from the detail modal and confirm the rollout reports paused;
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

  // Releases the review gate from the open detail modal (a primary modal
  // action, so it also dismisses the modal).
  async continueFromDetail() {
    await this.detailModal().getByRole("button", { name: "Continue rollout", exact: true }).click();
    await expect(this.detailModal()).toBeHidden();
    await this.validateTextInToast("Continuing");
  }

  // Aborts from the open detail modal through the confirmation dialog.
  async abortFromDetail() {
    await this.detailModal().getByRole("button", { name: "Abort", exact: true }).click();
    const dialog = this.page.getByTestId("abort-rollout-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Abort rollout", exact: true }).click();
    await expect(dialog).toBeHidden();
    await this.validateTextInToast("Aborted");
  }

  // The firmware picker of the model group shows this version.
  async validateModelAssignedFirmware(laneName: string, model: string, version: string | RegExp) {
    await expect(this.laneCard(laneName).getByTestId(`lane-firmware-select-${model}`)).toContainText(version);
  }

  // A history entry for this version carries the given outcome label.
  async validateHistoryOutcome(laneName: string, version: string, outcome: string) {
    await this.openLaneHistory(laneName);
    await expect(
      this.historyModal().locator("tr").filter({ hasText: version }).filter({ hasText: outcome }).first(),
    ).toBeVisible({ timeout: DEFAULT_TIMEOUT });
    await this.closeLaneHistory();
  }

  // Number of the model group's miners currently reporting this version.
  async countModelMinersOnVersion(laneName: string, model: string, version: string): Promise<number> {
    await this.openModelMiners(laneName, model);
    const rows = this.minersModal().locator('[data-testid^="lane-miner-"]');
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

  // The channel row's status column flags a pilot waiting for review.
  async validateChannelReviewNeeded(laneName: string) {
    await expect(this.channelRow(laneName).getByTestId(`channel-status-${laneName}`)).toContainText("review needed");
  }

  async validateLaneRolloutInProgress(laneName: string, version: string) {
    await expect(this.laneCard(laneName).getByText(`Rolling out ${version}`)).toBeVisible({
      timeout: DEFAULT_TIMEOUT,
    });
  }

  // The rollback action of the first history entry carrying this version.
  private historyRollbackButton(version: string): Locator {
    return this.historyModal()
      .locator("tr")
      .filter({ hasText: version })
      .getByRole("button", { name: "Roll back", exact: true })
      .first();
  }

  // A rollout history entry for this version offers a rollback action.
  async validateHistoryRollbackAvailable(laneName: string, version: string) {
    await this.openLaneHistory(laneName);
    await expect(this.historyRollbackButton(version)).toBeVisible();
    await this.closeLaneHistory();
  }

  // Restores a past version from the lane's rollout history via its entry's
  // rollback action and the confirmation dialog.
  async rollbackLaneFirmware(laneName: string, version: string) {
    await this.openLaneHistory(laneName);
    await this.historyRollbackButton(version).click();
    const dialog = this.page.getByTestId("rollback-firmware-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Roll back", exact: true }).click();
    await expect(dialog).toBeHidden();
    await this.validateTextInToast("Rolling back");
    await this.closeLaneHistory();
  }

  async validateLaneRolloutPill(laneName: string) {
    await expect(this.laneCard(laneName).getByTestId("lane-rollout-pill")).toBeVisible();
  }

  appRolloutPill(): Locator {
    return this.page.getByRole("button", { name: "View ongoing firmware rollouts" });
  }

  async validateAppRolloutPill() {
    await expect(this.appRolloutPill()).toBeVisible();
  }

  // Opens the app-header rollout pill popover and follows its link to the
  // release channels view.
  async followAppRolloutPillToLanes() {
    await this.appRolloutPill().click();
    await this.page.getByRole("link", { name: "View release channels", exact: true }).click();
    await this.validateTitle("Release channels");
    await expect(this.page.getByText("Loading release channels...", { exact: true })).toBeHidden();
  }

  // The rollout is done when every miner in the model group reports the
  // target version (checked in the live "View miners" modal), the progress
  // bar clears, and the rollout shows up as completed in the lane's history.
  async waitForLaneRolloutCompleted(laneName: string, model: string, version: string, timeoutMs: number) {
    const card = this.laneCard(laneName);
    await this.openModelMiners(laneName, model);
    const minerRows = this.minersModal().locator('[data-testid^="lane-miner-"]');
    const rowCount = await minerRows.count();
    for (let i = 0; i < rowCount; i++) {
      await expect(minerRows.nth(i)).toContainText(version, { timeout: timeoutMs });
    }
    await this.closeModelMiners();
    // Status flips on the next enforcement tick after the miners report in.
    await expect(card.getByText(`Rolling out ${version}`)).toBeHidden({ timeout: timeoutMs });
    await this.openLaneHistory(laneName);
    await expect(
      this.historyModal().locator("tr").filter({ hasText: "Completed" }).filter({ hasText: version }).first(),
    ).toBeVisible({ timeout: DEFAULT_TIMEOUT });
    await this.closeLaneHistory();
  }

  historyModal(): Locator {
    return this.page.getByTestId("modal");
  }

  async openLaneHistory(laneName: string) {
    await this.laneCard(laneName).getByRole("button", { name: "History", exact: true }).click();
    await this.validateTitleInModal("Rollout history");
  }

  async closeLaneHistory() {
    await this.historyModal().getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.historyModal()).toBeHidden();
  }

  // Deletes the channel from its open management card. Deleting returns to
  // the channels table, where the row must be gone.
  async deleteLane(laneName: string) {
    await this.laneCard(laneName).getByRole("button", { name: "Delete", exact: true }).click();
    const dialog = this.page.getByTestId("delete-lane-dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Delete channel", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(this.channelRow(laneName)).toBeHidden();
  }

  async deleteLaneIfPresent(laneName: string) {
    if (
      await this.channelRow(laneName)
        .isVisible()
        .catch(() => false)
    ) {
      await this.manageChannel(laneName);
      await this.deleteLane(laneName);
    }
  }

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
      await row.getByRole("button", { name: "Row actions" }).click();
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
