import { fromBinary } from "@bufbuild/protobuf";
import { type APIRequestContext, expect, type Page, type Request, type Route } from "@playwright/test";
import { firmwareTestImage } from "../../../src/protoOS/api/adapters/mdkV2/firmwareTestFixtures";
import type {
  V2GetContainerMapResponse,
  V2ListFirmwareUpdatesResponse,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";
import { StartFirmwareUpdateRequestSchema } from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/firmware_pb";

// Just over the controller's 1 MiB chunk limit, so the simulator's size and digest check covers a second chunk.
const imageBytes = 1024 * 1024 + 4096;

const procedure = (method: string) => `/mdk.v2.FirmwareService/${method}`;
/** What the controller requires on a call a test makes itself, outside the page. */
export const controllerHeaders = { "X-Frigate-Actor": "proto-os", "Connect-Protocol-Version": "1" };

/** Whether a request the page sent is this firmware RPC. */
export const isFirmwareCall = (request: Request, method: string) =>
  new URL(request.url()).pathname === procedure(method);

/** A Start the page sent, decoded so a test can check what it targets. */
export const startRequest = (request: Request) =>
  fromBinary(StartFirmwareUpdateRequestSchema, request.postDataBuffer()!);

export const containerMap = async (request: APIRequestContext) =>
  (await (await request.get("/api/v2/container/map")).json()) as V2GetContainerMapResponse;

/** Every update the controller knows, asked directly rather than through the page. */
export const listUpdates = async (request: APIRequestContext) =>
  (
    (await (
      await request.post(procedure("ListFirmwareUpdates"), { headers: controllerHeaders, data: {} })
    ).json()) as V2ListFirmwareUpdatesResponse
  ).updates ?? [];

/** Cancels the updates a test started, so the next test finds the simulator's boards idle. */
export async function cancelUpdates(request: APIRequestContext, taskIds: Iterable<string>) {
  for (const taskId of taskIds)
    expect(
      (await request.post(procedure("CancelFirmwareUpdate"), { headers: controllerHeaders, data: { taskId } })).ok(),
    ).toBe(true);
}

export class HashboardFirmwarePage {
  constructor(readonly page: Page) {}

  /** Present only while no update is tracked; a tracked update renames the entry point to its status. */
  uploadButton() {
    return this.page.getByRole("button", { name: "Upload hashboard firmware", exact: true });
  }
  statusButton() {
    return this.page.getByRole("button", { name: "Hashboard update status", exact: true });
  }
  /** The upload form or the update status: one dialog, titled by what it currently shows. */
  dialog() {
    return this.page.getByRole("dialog", { name: /^(Upload hashboard firmware|Hashboard update status)$/ });
  }
  statusDialog() {
    return this.page.getByRole("dialog", { name: "Hashboard update status", exact: true });
  }
  /** The question asked over the status dialog before an unconfirmed update is released. */
  releaseDialog() {
    return this.page.getByRole("dialog", { name: "Stop tracking this update?", exact: true });
  }

  status() {
    return this.dialog().getByRole("status");
  }
  alerts() {
    return this.dialog().getByRole("alert");
  }
  alert(text: string | RegExp) {
    return this.alerts().filter({ hasText: text });
  }
  boardRows() {
    return this.dialog().getByRole("listitem");
  }
  boardRow(text: string | RegExp) {
    return this.boardRows().filter({ hasText: text });
  }
  succeededRows() {
    return this.dialog().getByText("succeeded · 100%", { exact: true });
  }

  private button(name: string, dialog = this.dialog()) {
    return dialog.getByRole("button", { name, exact: true });
  }
  chooseFileButton() {
    return this.button("Choose file");
  }
  uploadAndInstallButton() {
    return this.button("Upload and install");
  }
  cancelUpdateButton() {
    return this.button("Cancel update");
  }
  retryCancelButton() {
    return this.button("Retry cancel");
  }
  stopTrackingButton() {
    return this.button("Stop tracking");
  }
  doneButton() {
    return this.button("Done");
  }
  closeButton() {
    return this.button("Close");
  }
  keepTrackingButton() {
    return this.button("Keep tracking", this.releaseDialog());
  }
  confirmStopTrackingButton() {
    return this.button("Stop tracking", this.releaseDialog());
  }

  /** Resolves once the question has finished animating in and its buttons can be pressed. */
  async releaseDialogSettled() {
    await this.confirmStopTrackingButton().click({ trial: true });
  }

  async open() {
    await this.page.goto("/settings/general");
    await this.uploadButton().click();
  }
  async choose(hardware = "f1") {
    await this.dialog()
      .getByLabel("Hashboard firmware file")
      .setInputFiles({
        name: "f1-app-prod.bin",
        mimeType: "application/octet-stream",
        buffer: Buffer.from(firmwareTestImage("prod", hardware, imageBytes)),
      });
  }
  /** Chooses a valid image and waits for the confirmation summary, without starting the update. */
  async select() {
    await this.choose();
    await expect(this.dialog().getByText("Version 9.0.0 · prod · 18 hashboards")).toBeVisible();
  }
  async confirm() {
    await this.uploadAndInstallButton().click();
  }
  async install() {
    await this.select();
    await this.confirm();
  }
  async reopenStatus() {
    await this.page.reload();
    await this.statusButton().click();
  }

  /** Collects the task ID of every Start this page sends, into `taskIds` when several pages share one set. */
  startedTaskIds(taskIds = new Set<string>()) {
    this.page.on("request", (request) => {
      if (isFirmwareCall(request, "StartFirmwareUpdate")) taskIds.add(startRequest(request).taskId);
    });
    return taskIds;
  }
  /** Lets a test answer this page's Starts itself: refuse one, lose its response, or check its payload. */
  async interceptStart(handler: (route: Route) => Promise<void>) {
    await this.page.route(`**${procedure("StartFirmwareUpdate")}`, handler);
  }
  /** The controller's answer to the next Start this page sends. */
  startResponse() {
    return this.page.waitForResponse((response) => isFirmwareCall(response.request(), "StartFirmwareUpdate"));
  }
}
