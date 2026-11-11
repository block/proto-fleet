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
