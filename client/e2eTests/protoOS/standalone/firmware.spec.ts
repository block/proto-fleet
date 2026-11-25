import { expect, test } from "@playwright/test";
import {
  cancelUpdates,
  containerMap,
  controllerHeaders,
  HashboardFirmwarePage,
  isFirmwareCall,
  listUpdates,
  startRequest,
} from "../pages/hashboardFirmware";

// Separate complete six-tank simulator; the schematic suite retains its small map.
test.use({ baseURL: "http://127.0.0.1:18082" });

test("local F1 upload recovers a lost Start response and reload, reports every board, and preserves controller software", async ({
  page,
  request,
}) => {
  const macs = (await containerMap(request)).containerMap!.tanks!.flatMap((tank) =>
    tank.modules!.flatMap((module) =>
      module.slots!.filter((slot) => slot.occupied).map((slot) => slot.hashboard!.macAddress!),
    ),
  );
  const before = await (await request.get("/api/v2/system/info")).json();
  const dashboard = new HashboardFirmwarePage(page);
  const taskIds = dashboard.startedTaskIds();
  let startCount = 0;
  let chunkWrites = 0;
  page.on("request", (sent) => {
    if (isFirmwareCall(sent, "WriteFirmwareArtifactUploadChunk")) chunkWrites++;
  });
  await dashboard.interceptStart(async (route) => {
    startCount++;
    const start = startRequest(route.request());
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
    await expect(dashboard.alerts()).toContainText("valid F1");
    await expect(dashboard.uploadAndInstallButton()).toBeDisabled();
    await dashboard.install();
    await expect(dashboard.alert("Lost response")).toBeVisible();
    await dashboard.reopenStatus();
    await expect(dashboard.status()).toHaveText("Update finished");
    await expect(dashboard.boardRows()).toHaveCount(18);
    await expect(dashboard.succeededRows()).toHaveCount(18);
    await test.info().attach("firmware-completed", { body: await page.screenshot(), contentType: "image/png" });
    expect(startCount).toBe(1);
    // Start is only reachable after the simulator accepted both chunks against the declared size and digest.
    expect(chunkWrites).toBe(2);
    const afterInfo = await (await request.get("/api/v2/system/info")).json();
    expect(afterInfo.info.version).toBe(before.info.version);
    expect(afterInfo.info.osInfo).toEqual(before.info.osInfo);
    await dashboard.doneButton().click();
    await expect(dashboard.uploadButton()).toBeVisible();
  } finally {
    await cancelUpdates(request, taskIds);
    // Same-version force installs exercise flashing while preserving the baseline.
    const after = await containerMap(request);
    expect(
      after.containerMap!.tanks!.flatMap((tank) =>
        tank.modules!.flatMap((module) => module.slots!.map((slot) => slot.hashboard!.firmwareVersion)),
      ),
    ).toEqual(Array(18).fill("9.0.0"));
  }
});

test("local hashboard firmware can cancel an update and displays a board failure", async ({ page, request }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  const dashboard = new HashboardFirmwarePage(page);
  const mac = (await containerMap(request)).containerMap!.tanks![0].modules![0].slots![0].hashboard!.macAddress!;
  const isolation = async (isolated: boolean) => {
    expect(
      (
        await request.post("/mdk.v2.MiningService/SetIsolation", {
          headers: controllerHeaders,
          data: { scope: { identifiers: [mac] }, isolated },
        })
      ).ok(),
    ).toBe(true);
  };
  const taskIds = dashboard.startedTaskIds();
  try {
    await dashboard.open();
    await dashboard.install();
    await dashboard.cancelUpdateButton().click();
    await expect(dashboard.chooseFileButton()).toBeVisible();
    const canceled = (await listUpdates(request)).find((job) => taskIds.has(job.taskId!));
    expect(canceled?.status).toBe("FIRMWARE_UPDATE_STATUS_CANCELED");
    await isolation(true);
    await dashboard.install();
    await expect(dashboard.status()).toHaveText("Update finished with errors");
    await expect(dashboard.boardRow(/failed.*Simulated hashboard/)).toBeVisible();
    await expect(dashboard.succeededRows()).toHaveCount(17);
    await test.info().attach("firmware-failed-dark", { body: await page.screenshot(), contentType: "image/png" });
  } finally {
    await cancelUpdates(request, taskIds);
    await isolation(false);
  }
});

test("a refused Start is not tracked, and an update no controller knows stays tracked until the operator stops tracking", async ({
  page,
  request,
}) => {
  const dashboard = new HashboardFirmwarePage(page);
  const taskIds = dashboard.startedTaskIds();
  let refuse = true;
  // Neither Start reaches the simulator: the first is definitively refused, the second is lost on the way.
  await dashboard.interceptStart(async (route) => {
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
    await expect(dashboard.alerts()).toHaveText("hashboard update already running");
    await expect(dashboard.uploadAndInstallButton()).toBeEnabled();
    await page.reload();
    await expect(dashboard.uploadButton()).toBeVisible();
  });

  await test.step("a lost Start stays tracked after every controller answers not found", async () => {
    refuse = false;
    await dashboard.uploadButton().click();
    await dashboard.install();
    await expect(dashboard.alert(/Update not found/)).toBeVisible();
    await dashboard.cancelUpdateButton().click();
    await expect(dashboard.status()).toHaveText("The cancel was not confirmed by every controller.");
    await expect(dashboard.stopTrackingButton()).toBeEnabled();
    await test.info().attach("firmware-cancel-unconfirmed", {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    // With three buttons the footer stacks; the board list shrinks so the buttons stay on screen in a short window.
    const size = page.viewportSize()!;
    await page.setViewportSize({ width: size.width, height: 700 });
    await expect(dashboard.closeButton()).toBeInViewport({ ratio: 0.99 });
    await page.setViewportSize(size);
    await dashboard.reopenStatus();
    // The unconfirmed cancel is saved with the record, so a reload shows the same state and the same way out.
    await expect(dashboard.status()).toHaveText("The cancel was not confirmed by every controller.");
    await expect(dashboard.retryCancelButton()).toBeVisible();
  });

  await test.step("the operator releases it explicitly", async () => {
    await dashboard.stopTrackingButton().click();
    await expect(dashboard.releaseDialog()).toBeVisible();
    // The question opens on the button that keeps the record, so a repeated key press cannot release it, and
    // declining returns to the button that asked.
    await expect(dashboard.keepTrackingButton()).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(dashboard.releaseDialog()).toBeHidden();
    await expect(dashboard.stopTrackingButton()).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(dashboard.releaseDialog()).toBeVisible();
    // The question animates in over the status dialog; capture it once it has settled.
    await dashboard.releaseDialogSettled();
    await test.info().attach("firmware-stop-tracking", { body: await page.screenshot(), contentType: "image/png" });
    await dashboard.confirmStopTrackingButton().click();
    await expect(dashboard.chooseFileButton()).toBeVisible();
    await page.reload();
    await expect(dashboard.uploadButton()).toBeVisible();
  });

  expect(taskIds.size).toBe(2);
  expect((await listUpdates(request)).filter((job) => taskIds.has(job.taskId!))).toEqual([]);
});

test("a second tab adopts the tracked update instead of starting another one", async ({ page, context, request }) => {
  const first = new HashboardFirmwarePage(page);
  const second = new HashboardFirmwarePage(await context.newPage());
  const taskIds = first.startedTaskIds();
  second.startedTaskIds(taskIds);
  try {
    // Both tabs select the image before either starts, so the second still shows the upload form.
    await first.open();
    await first.select();
    await second.open();
    await second.select();

    await first.confirm();
    // The dialog switches to the status view once the record is saved, just before Start is sent.
    await expect(first.statusDialog()).toBeVisible();
    await second.confirm();
    await expect(second.alerts()).toContainText("already tracked");
    await expect(second.status()).toHaveText("Update finished");
    await expect(second.succeededRows()).toHaveCount(18);
    expect(taskIds.size).toBe(1);

    // The second tab tracks the same task, so finishing there releases the record for the first tab too.
    await second.doneButton().click();
    await first.page.reload();
    await expect(first.uploadButton()).toBeVisible();
  } finally {
    await cancelUpdates(request, taskIds);
  }
});

test("an update another client cancels reads as canceled, and the dialog fits a short window", async ({
  page,
  request,
}) => {
  const dashboard = new HashboardFirmwarePage(page);
  const taskIds = dashboard.startedTaskIds();
  try {
    await dashboard.open();
    const started = dashboard.startResponse();
    await dashboard.install();
    expect((await started).ok()).toBe(true);
    // Another client, such as a second browser or Fleet, cancels the task this page is tracking.
    await cancelUpdates(request, taskIds);
    await expect(dashboard.status()).toHaveText("Update canceled");
    await expect(dashboard.boardRows()).toHaveCount(18);

    // The board list shrinks before the dialog has to scroll, so the footer stays on screen in a short window.
    await page.setViewportSize({ width: page.viewportSize()!.width, height: 640 });
    await expect(dashboard.doneButton()).toBeInViewport({ ratio: 0.99 });
    await test.info().attach("firmware-canceled-short-window", {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    await dashboard.doneButton().click();
    await expect(dashboard.uploadButton()).toBeVisible();
  } finally {
    await cancelUpdates(request, taskIds);
  }
});
