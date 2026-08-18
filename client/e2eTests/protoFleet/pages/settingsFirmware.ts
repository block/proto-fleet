import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

type FirmwareUploadMetadata = {
  manufacturer: string;
  model: string;
  firmwareVersion: string;
};

type CreateLaneRequestBody = {
  deviceIdentifiers?: string[];
};

type RolloutApiMember = {
  state: string;
};

type RolloutApiRecord = {
  rolloutId: string;
  state: string;
  revision: string;
  members: RolloutApiMember[];
};

type RolloutResponseBody = {
  rollout?: RolloutApiRecord;
};

const ROLLOUT_SERVICE = "/api-proxy/rollout.v1.RolloutService";
const DEVICE_SET_SERVICE = "/api-proxy/device_set.v1.DeviceSetService";
const ROLLOUT_SETTLEMENT_TIMEOUT = DEFAULT_TIMEOUT * 8;

export class SettingsFirmwarePage extends BasePage {
  async validateFirmwarePageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/firmware/);
    await this.validateTitle("Firmware");
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

  async openRolloutLanesTab() {
    await this.page.getByRole("button", { name: "Rollout lanes", exact: true }).click();
    await expect(this.page.getByText("Stable firmware lanes", { exact: true })).toBeVisible();
    await expect(this.page.getByText("Loading rollout lanes...", { exact: true })).toBeHidden();
  }

  async validateRolloutLanesSurface() {
    await expect(this.page.getByRole("button", { name: "Create lane", exact: true })).toBeVisible();
    await expect(this.page.getByText("Stable firmware lanes", { exact: true })).toBeVisible();
  }

  async createRolloutLane(input: {
    label: string;
    description: string;
    sourceFirmwareFileName: string;
    minerIpAddresses: string[];
    onLaneCreated?: (deviceIdentifiers: string[]) => void;
  }): Promise<CreateLaneRequestBody> {
    await this.page.getByRole("button", { name: "Create lane", exact: true }).click();
    const modal = this.page.getByTestId("full-screen-two-pane-modal");
    await expect(modal).toBeVisible();
    await expect(modal.getByText("Create rollout lane", { exact: true })).toBeVisible();

    await modal.getByLabel("Lane name").fill(input.label);
    await modal.getByLabel("Description").fill(input.description);
    await modal.getByText(input.sourceFirmwareFileName, { exact: true }).click();
    await modal.getByRole("button", { name: "Select miners", exact: true }).click();

    const selectionModal = this.page.getByTestId("modal").filter({ hasText: "Select miners" });
    await expect(selectionModal).toBeVisible();
    for (const ipAddress of input.minerIpAddresses) {
      const minerRow = selectionModal.getByTestId("list-body").locator("tr").filter({ hasText: ipAddress });
      await expect(minerRow).toHaveCount(1);
      await minerRow.locator('input[type="checkbox"]').check();
    }
    await selectionModal.getByRole("button", { name: "Done", exact: true }).click();
    await expect(selectionModal).toBeHidden();

    const createRequestPromise = this.page.waitForRequest(
      (request) =>
        request.method() === "POST" &&
        request.url().includes("/rollout.v1.RolloutService/CreateRolloutLane") &&
        request.postDataJSON().label === input.label,
    );
    const createResponsePromise = this.page.waitForResponse(
      (response) =>
        response.ok() &&
        response.request().method() === "POST" &&
        response.url().includes("/rollout.v1.RolloutService/CreateRolloutLane") &&
        response.request().postDataJSON().label === input.label,
    );
    await modal.getByRole("button", { name: "Create lane", exact: true }).click();
    const [createRequest] = await Promise.all([createRequestPromise, createResponsePromise]);
    const createRequestBody = createRequest.postDataJSON() as CreateLaneRequestBody;
    input.onLaneCreated?.(createRequestBody.deviceIdentifiers ?? []);
    await expect(modal).toBeHidden();
    await expect(this.rolloutLaneRow(input.label)).toBeVisible();

    return createRequestBody;
  }

  async validateRolloutLane(label: string, releaseText: string, memberCount: number) {
    const row = this.rolloutLaneRow(label);
    await expect(row).toBeVisible();
    await expect(row).toContainText(releaseText);
    await expect(row).toContainText(memberCount.toLocaleString());
  }

  async startTwoBatchRollout(input: {
    laneLabel: string;
    rolloutName: string;
    reason: string;
    targetRelease: FirmwareUploadMetadata & {
      fileName: string;
    };
    onRolloutCreated?: (rolloutId: string) => void;
  }): Promise<RolloutApiRecord> {
    await this.rolloutLaneRow(input.laneLabel)
      .getByRole("button", { name: `Start rollout for ${input.laneLabel}`, exact: true })
      .click();

    const modal = this.page.getByTestId("full-screen-two-pane-modal");
    await expect(modal).toBeVisible();
    await expect(modal.getByText("Start firmware rollout", { exact: true })).toBeVisible();
    await modal.getByLabel("Name").fill(input.rolloutName);
    await modal.getByLabel("Reason").fill(input.reason);
    await modal.getByLabel(`${input.targetRelease.manufacturer} ${input.targetRelease.model}`, { exact: true }).click();
    await this.page
      .getByRole("option", {
        name: `${input.targetRelease.firmwareVersion} (${input.targetRelease.fileName})`,
        exact: true,
      })
      .click();
    await modal.getByLabel("Method").click();
    await this.page.getByRole("option", { name: "Multiple batches", exact: true }).click();
    await modal.getByLabel("Batch size (miners)").fill("1");
    await expect(modal.getByText("About 2 batches, review after each batch", { exact: true })).toBeVisible();

    const startResponsePromise = this.page.waitForResponse(
      (response) =>
        response.ok() &&
        response.request().method() === "POST" &&
        response.url().includes("/rollout.v1.RolloutService/StartRolloutLane"),
    );
    const admitResponsePromise = this.page.waitForResponse(
      (response) =>
        response.ok() &&
        response.request().method() === "POST" &&
        response.url().includes("/rollout.v1.RolloutService/AdmitRollout"),
    );
    await modal.getByRole("button", { name: "Start rollout", exact: true }).click();
    const startResponse = await startResponsePromise;
    const responseBody = (await startResponse.json()) as RolloutResponseBody;
    if (!responseBody.rollout) {
      throw new Error("StartRolloutLane response did not include a rollout.");
    }
    input.onRolloutCreated?.(responseBody.rollout.rolloutId);
    await admitResponsePromise;
    await expect(modal).toBeHidden();

    return responseBody.rollout;
  }

  async validateMembershipSplit(sourceRemaining: number, targetConfirmed: number) {
    await expect(
      this.page.getByText(`${sourceRemaining.toLocaleString()} miners remain on the current release`, {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      this.page.getByText(`${targetConfirmed.toLocaleString()} miners confirmed and moved`, { exact: true }),
    ).toBeVisible();
  }

  async waitForMembershipSplit(sourceRemaining: number, targetConfirmed: number) {
    await expect(
      this.page.getByText(`${sourceRemaining.toLocaleString()} miners remain on the current release`, {
        exact: true,
      }),
    ).toBeVisible({ timeout: ROLLOUT_SETTLEMENT_TIMEOUT });
    await expect(
      this.page.getByText(`${targetConfirmed.toLocaleString()} miners confirmed and moved`, { exact: true }),
    ).toBeVisible({ timeout: ROLLOUT_SETTLEMENT_TIMEOUT });
  }

  async waitForRolloutStage(stage: "Review" | "Paused" | "Completed" | "Aborted" | "Reverted") {
    await expect(this.page.getByTestId("active-rollout-primary-lockup")).toContainText(stage, {
      timeout: ROLLOUT_SETTLEMENT_TIMEOUT,
    });
  }

  async pauseAndResumeReview() {
    await this.page.getByRole("button", { name: "Pause", exact: true }).click();
    await this.waitForRolloutStage("Paused");
    await this.page.getByRole("button", { name: "Resume", exact: true }).click();
    await this.waitForRolloutStage("Review");
  }

  async continueRollout() {
    await this.page.getByRole("button", { name: "Continue", exact: true }).click();
  }

  async abortRollout() {
    await this.page.getByTestId("active-rollout-more-actions-trigger").click();
    await this.page.getByTestId("active-rollout-abort-action").click();
    await this.page.getByTestId("confirm-abort-rollout").click();
    await this.waitForRolloutStage("Aborted");
  }

  async revertRollout() {
    await this.page.getByRole("button", { name: "Revert", exact: true }).click();
    await this.page.getByTestId("confirm-revert-rollout").click();
    await this.waitForRolloutStage("Reverted");
  }

  async reloadAndReopenRollout(laneLabel: string, rolloutName: string) {
    await this.page.reload();
    await this.validateFirmwarePageOpened();
    await this.openRolloutLanesTab();
    await this.rolloutLaneRow(laneLabel)
      .getByRole("button", { name: `View latest rollout for ${laneLabel}`, exact: true })
      .click();
    const modal = this.page.getByTestId("view-rollout-modal");
    await expect(modal).toBeVisible();
    await expect(modal.getByText(rolloutName, { exact: true })).toBeVisible();
    await modal.getByLabel("Close dialog").click();
    await expect(modal).toBeHidden();
  }

  async validateNoRetryAction() {
    await expect(this.page.getByRole("button", { name: /retry/i })).toHaveCount(0);
  }

  async cleanupRollout(rolloutId: string | undefined, deviceIdentifiers: string[]) {
    if (rolloutId) {
      let rollout = await this.getRollout(rolloutId);
      if (this.isActiveRolloutState(rollout.state)) {
        await this.runRolloutControl("AbortRollout", rollout, "E2E cleanup abort");
      }

      await this.waitForRolloutMembersToLeaveState(rolloutId, "ROLLOUT_MEMBER_STATE_ADMITTED");

      rollout = await this.getRollout(rolloutId);
      if (rollout.state === "ROLLOUT_STATE_REVERTING") {
        await this.waitForRolloutMembersToLeaveState(rolloutId, "ROLLOUT_MEMBER_STATE_REVERTING");
        rollout = await this.getRollout(rolloutId);
        if (rollout.state === "ROLLOUT_STATE_REVERTING") {
          await this.runRolloutControl("CompleteRollout", rollout, "E2E cleanup finish active revert");
          rollout = await this.getRollout(rolloutId);
        }
      }
      if (
        rollout.state !== "ROLLOUT_STATE_REVERTED" &&
        rollout.members.some((member) => member.state === "ROLLOUT_MEMBER_STATE_SUCCEEDED")
      ) {
        if (this.isActiveRolloutState(rollout.state)) {
          rollout = await this.runRolloutControl("AbortRollout", rollout, "E2E cleanup abort before revert");
        }
        await this.runRolloutControl("RevertRollout", rollout, "E2E cleanup restore source release");

        await this.waitForRolloutMembersToLeaveState(rolloutId, "ROLLOUT_MEMBER_STATE_REVERTING");

        rollout = await this.getRollout(rolloutId);
        if (rollout.state === "ROLLOUT_STATE_REVERTING") {
          await this.runRolloutControl("CompleteRollout", rollout, "E2E cleanup finish revert");
        }
      }
    }

    if (deviceIdentifiers.length > 0) {
      await this.connectPost(`${DEVICE_SET_SERVICE}/AssignDevicesToChannel`, {
        deviceSelector: {
          deviceList: {
            deviceIdentifiers,
          },
        },
      });
    }
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

  private rolloutLaneRow(label: string) {
    return this.page
      .getByTestId("list-body")
      .locator("tr")
      .filter({ has: this.page.getByText(label, { exact: true }) });
  }

  private async getRollout(rolloutId: string): Promise<RolloutApiRecord> {
    const response = await this.connectPost<RolloutResponseBody>(`${ROLLOUT_SERVICE}/GetRollout`, { rolloutId });
    if (!response.rollout) {
      throw new Error(`GetRollout response did not include rollout ${rolloutId}.`);
    }
    return response.rollout;
  }

  private async waitForRolloutMembersToLeaveState(rolloutId: string, state: string) {
    await expect
      .poll(
        async () => {
          const current = await this.getRollout(rolloutId);
          return current.members.filter((member) => member.state === state).length;
        },
        { timeout: ROLLOUT_SETTLEMENT_TIMEOUT, intervals: [DEFAULT_INTERVAL] },
      )
      .toBe(0);
  }

  private async runRolloutControl(
    operation: "AbortRollout" | "RevertRollout" | "CompleteRollout",
    rollout: RolloutApiRecord,
    reason: string,
  ): Promise<RolloutApiRecord> {
    const response = await this.connectPost<RolloutResponseBody>(`${ROLLOUT_SERVICE}/${operation}`, {
      rolloutId: rollout.rolloutId,
      expectedRevision: rollout.revision,
      idempotencyKey: `firmware-rollout-e2e-${operation.toLowerCase()}-${crypto.randomUUID()}`,
      reason,
    });
    if (!response.rollout) {
      throw new Error(`${operation} response did not include rollout ${rollout.rolloutId}.`);
    }
    return response.rollout;
  }

  private isActiveRolloutState(state: string): boolean {
    return ["ROLLOUT_STATE_CREATED", "ROLLOUT_STATE_RUNNING", "ROLLOUT_STATE_PAUSED", "ROLLOUT_STATE_REVIEW"].includes(
      state,
    );
  }

  private async connectPost<T>(procedure: string, body: unknown): Promise<T> {
    const result = await this.page.evaluate(
      async ({ procedure, body }) => {
        const response = await fetch(procedure, {
          method: "POST",
          headers: {
            "Connect-Protocol-Version": "1",
            "Content-Type": "application/json",
          },
          credentials: "include",
          body: JSON.stringify(body),
        });
        return {
          ok: response.ok,
          status: response.status,
          text: await response.text(),
        };
      },
      { procedure, body },
    );

    if (!result.ok) {
      throw new Error(`Connect request ${procedure} failed with ${result.status}: ${result.text}`);
    }
    return JSON.parse(result.text) as T;
  }
}
