import { fromBinary } from "@bufbuild/protobuf";
import { expect, test } from "@playwright/test";
import type {
  V2GetContainerMapResponse,
  V2ListFirmwareUpdatesResponse,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";
import { StartFirmwareUpdateRequestSchema } from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/firmware_pb";
import { CT1FirmwarePage } from "../pages/ct1Firmware";

// Separate complete six-tank simulator; the schematic suite retains its small map.
test.use({ baseURL: "http://127.0.0.1:18082" });
const procedure = (method: string) => `/mdk.v2.FirmwareService/${method}`;
const headers = { "X-Frigate-Actor": "proto-os", "Connect-Protocol-Version": "1" };

test("local F1 upload recovers a lost Start response and reload, reports every board, and preserves controller software", async ({
  page,
  request,
}) => {
  const map = (await (await request.get("/api/v2/container/map")).json()) as V2GetContainerMapResponse;
  const macs = map.containerMap!.tanks!.flatMap((tank) =>
    tank.modules!.flatMap((module) =>
      module.slots!.filter((slot) => slot.occupied).map((slot) => slot.hashboard!.macAddress!),
    ),
  );
  const before = await (await request.get("/api/v2/system/info")).json();
  const dashboard = new CT1FirmwarePage(page);
  let startCount = 0;
  let chunkWrites = 0;
  page.on("request", (req) => {
    if (new URL(req.url()).pathname === procedure("WriteFirmwareArtifactUploadChunk")) chunkWrites++;
  });
  const taskIds = new Set<string>();
  await page.route(`**${procedure("StartFirmwareUpdate")}`, async (route) => {
    startCount++;
    const start = fromBinary(StartFirmwareUpdateRequestSchema, route.request().postDataBuffer()!);
    taskIds.add(start.taskId);
    expect(start.scope?.identifiers.sort()).toEqual(macs.sort());
    expect(start.options?.force).toBe(true);
    expect(start.options?.build).toBe("prod");
    expect(route.request().headers()["x-frigate-actor"]).toBe("proto-os");
    expect(route.request().headers().authorization).toBeUndefined();
    expect((await route.fetch()).ok()).toBe(true);
    // The controller accepted it but the browser didn't receive confirmation.
    await route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ code: "unavailable", message: "Lost response" }),
    });
  });
  try {
    await dashboard.open();
    await dashboard.choose("r2");
    await expect(dashboard.dialog().getByRole("alert")).toContainText("valid F1");
    await expect(dashboard.dialog().getByRole("button", { name: "Upload and install" })).toBeDisabled();
    await dashboard.install();
    await expect(dashboard.dialog().getByText("Lost response")).toBeVisible();
    await dashboard.reopenStatus();
    await expect(dashboard.dialog().getByRole("status")).toHaveText("Update finished");
    await expect(dashboard.dialog().getByRole("listitem")).toHaveCount(18);
    await expect(dashboard.dialog().getByText("succeeded · 100%", { exact: true })).toHaveCount(18);
    await test.info().attach("firmware-completed", { body: await page.screenshot(), contentType: "image/png" });
    expect(startCount).toBe(1);
    // Start is only reachable after the simulator accepted both chunks against the declared size and digest.
    expect(chunkWrites).toBe(2);
    const afterInfo = await (await request.get("/api/v2/system/info")).json();
    expect(afterInfo.info.version).toBe(before.info.version);
    expect(afterInfo.info.osInfo).toEqual(before.info.osInfo);
    await dashboard.dialog().getByRole("button", { name: "Done", exact: true }).click();
    await expect(page.getByRole("button", { name: "Upload hashboard firmware", exact: true })).toBeVisible();
  } finally {
    for (const taskId of taskIds)
      expect((await request.post(procedure("CancelFirmwareUpdate"), { headers, data: { taskId } })).ok()).toBe(true);
    // Same-version force installs exercise flashing while preserving the baseline.
    const after = (await (await request.get("/api/v2/container/map")).json()) as V2GetContainerMapResponse;
    expect(
      after.containerMap!.tanks!.flatMap((tank) =>
        tank.modules!.flatMap((module) => module.slots!.map((slot) => slot.hashboard!.firmwareVersion)),
      ),
    ).toEqual(Array(18).fill("9.0.0"));
  }
});

test("local hashboard firmware can cancel an update and displays a board failure", async ({ page, request }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  const dashboard = new CT1FirmwarePage(page);
  const map = (await (await request.get("/api/v2/container/map")).json()) as V2GetContainerMapResponse;
  const mac = map.containerMap!.tanks![0].modules![0].slots![0].hashboard!.macAddress!;
  const isolation = async (isolated: boolean) => {
    expect(
      (
        await request.post("/mdk.v2.MiningService/SetIsolation", {
          headers,
          data: { scope: { identifiers: [mac] }, isolated },
        })
      ).ok(),
    ).toBe(true);
  };
  const taskIds = new Set<string>();
  page.on("request", (req) => {
    if (new URL(req.url()).pathname === procedure("StartFirmwareUpdate"))
      taskIds.add(fromBinary(StartFirmwareUpdateRequestSchema, req.postDataBuffer()!).taskId);
  });
  try {
    await dashboard.open();
    await dashboard.install();
    await dashboard.dialog().getByRole("button", { name: "Cancel update", exact: true }).click();
    await expect(dashboard.dialog().getByRole("button", { name: "Choose file" })).toBeVisible();
    const listed = (await (
      await request.post(procedure("ListFirmwareUpdates"), { headers, data: {} })
    ).json()) as V2ListFirmwareUpdatesResponse;
    expect(listed.updates!.find((job) => taskIds.has(job.taskId!))?.status).toBe("FIRMWARE_UPDATE_STATUS_CANCELED");
    await isolation(true);
    await dashboard.install();
    await expect(dashboard.dialog().getByRole("status")).toHaveText("Update finished");
    await expect(dashboard.dialog().getByText(/failed.*Simulated hashboard/)).toBeVisible();
    await expect(dashboard.dialog().getByText("succeeded · 100%", { exact: true })).toHaveCount(17);
    await test.info().attach("firmware-failed-dark", { body: await page.screenshot(), contentType: "image/png" });
  } finally {
    for (const taskId of taskIds)
      expect((await request.post(procedure("CancelFirmwareUpdate"), { headers, data: { taskId } })).ok()).toBe(true);
    await isolation(false);
  }
});

test("a refused Start is not tracked, and an update no controller knows stays tracked until the operator stops tracking", async ({
  page,
  request,
}) => {
  const dashboard = new CT1FirmwarePage(page);
  const taskIds = new Set<string>();
  let refuse = true;
  // Neither Start reaches the simulator: the first is definitively refused, the second is lost on the way.
  await page.route(`**${procedure("StartFirmwareUpdate")}`, async (route) => {
    taskIds.add(fromBinary(StartFirmwareUpdateRequestSchema, route.request().postDataBuffer()!).taskId);
    if (refuse)
      await route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({ code: "failed_precondition", message: "hashboard update already running" }),
      });
    else await route.abort("connectionfailed");
  });

  await test.step("a refused Start keeps the selection and saves nothing", async () => {
    await dashboard.open();
    await dashboard.install();
    await expect(dashboard.dialog().getByRole("alert")).toHaveText("hashboard update already running");
    await expect(dashboard.dialog().getByRole("button", { name: "Upload and install" })).toBeEnabled();
    await page.reload();
    await expect(dashboard.uploadButton()).toBeVisible();
  });

  await test.step("a lost Start stays tracked after every controller answers not found", async () => {
    refuse = false;
    await dashboard.uploadButton().click();
    await dashboard.install();
    await expect(dashboard.dialog().getByText(/Update not found/)).toBeVisible();
    await dashboard.dialog().getByRole("button", { name: "Cancel update", exact: true }).click();
    await expect(dashboard.dialog().getByRole("status")).toHaveText(
      "The cancel was not confirmed by every controller.",
    );
    await expect(dashboard.dialog().getByRole("button", { name: "Stop tracking", exact: true })).toBeEnabled();
    await test.info().attach("firmware-cancel-unconfirmed", {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    await dashboard.reopenStatus();
    await expect(dashboard.dialog().getByRole("button", { name: "Retry cancel", exact: true })).toBeVisible();
  });

  await test.step("the operator releases it explicitly", async () => {
    await dashboard.dialog().getByRole("button", { name: "Retry cancel", exact: true }).click();
    await dashboard.dialog().getByRole("button", { name: "Stop tracking", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "Stop tracking this update?" })).toBeVisible();
    await test.info().attach("firmware-stop-tracking", { body: await page.screenshot(), contentType: "image/png" });
    await dashboard.dialog().getByRole("button", { name: "Stop tracking", exact: true }).click();
    await expect(dashboard.dialog().getByRole("button", { name: "Choose file" })).toBeVisible();
    await page.reload();
    await expect(dashboard.uploadButton()).toBeVisible();
  });

  expect(taskIds.size).toBe(2);
  const listed = (await (
    await request.post(procedure("ListFirmwareUpdates"), { headers, data: {} })
  ).json()) as V2ListFirmwareUpdatesResponse;
  expect((listed.updates ?? []).filter((job) => taskIds.has(job.taskId!))).toEqual([]);
});

test("a second tab adopts the tracked update instead of starting another one", async ({ page, context, request }) => {
  const first = new CT1FirmwarePage(page);
  const second = new CT1FirmwarePage(await context.newPage());
  const taskIds = new Set<string>();
  for (const tab of [first.page, second.page])
    tab.on("request", (req) => {
      if (new URL(req.url()).pathname === procedure("StartFirmwareUpdate"))
        taskIds.add(fromBinary(StartFirmwareUpdateRequestSchema, req.postDataBuffer()!).taskId);
    });
  try {
    // Both tabs select the image before either starts, so the second still shows the upload form.
    await first.open();
    await first.select();
    await second.open();
    await second.select();

    await first.confirm();
    // The dialog switches to the status view once the record is saved, just before Start is sent.
    await expect(first.page.getByRole("dialog", { name: "Hashboard update status" })).toBeVisible();
    await second.confirm();
    await expect(second.dialog().getByRole("alert")).toContainText("already tracked");
    await expect(second.dialog().getByRole("status")).toHaveText("Update finished");
    await expect(second.dialog().getByText("succeeded · 100%", { exact: true })).toHaveCount(18);
    expect(taskIds.size).toBe(1);

    // The second tab tracks the same task, so finishing there releases the record for the first tab too.
    await second.dialog().getByRole("button", { name: "Done", exact: true }).click();
    await first.page.reload();
    await expect(first.uploadButton()).toBeVisible();
  } finally {
    for (const taskId of taskIds)
      expect((await request.post(procedure("CancelFirmwareUpdate"), { headers, data: { taskId } })).ok()).toBe(true);
  }
});
