import { fromBinary } from "@bufbuild/protobuf";
import { type APIRequestContext, expect, type Request, type Response, test } from "@playwright/test";
import type { V2GetContainerMapResponse } from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";
import type { V2GetContainerControlsResponse } from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";
import {
  SetContainerControlRequestSchema,
  SetContainerSetpointRequestSchema,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/power_pb";
import { ContainerStandalonePage } from "../pages/containerStandalone";

async function miningStates(request: APIRequestContext) {
  const response = await request.get("/api/v2/container/map");
  expect(response.ok()).toBe(true);
  const map = (await response.json()) as V2GetContainerMapResponse;
  const tanks = map.containerMap?.tanks;
  expect(tanks).toHaveLength(2);
  return tanks!.flatMap((tank) => {
    expect(tank.id).toBeDefined();
    expect(tank.modules).toHaveLength(2);
    return tank.modules!.flatMap((module) => {
      expect(module.id).toBeDefined();
      expect(module.slots).toHaveLength(3);
      return module.slots!.map(({ hashboard }) => {
        expect(hashboard?.macAddress).toBeTruthy();
        expect(hashboard?.miningState).toBeDefined();
        return { tank: tank.id!, module: module.id!, mac: hashboard!.macAddress!, state: hashboard!.miningState! };
      });
    });
  });
}

function expectDirectRequest(request: Request, baseURL: string) {
  expect(new URL(request.url()).origin).toBe(new URL(baseURL).origin);
  expect(request.headers().authorization).toBeUndefined();
  expect(request.headers()["x-frigate-actor"]).toBe("proto-os");
}

function isSupersededOptionalAsset(
  request: Request,
  assetDocuments: Map<Request, number>,
  requiredAssets: Map<number, Set<string>>,
  documentGeneration: number,
) {
  const generation = assetDocuments.get(request)!;
  return generation < documentGeneration && !requiredAssets.get(generation)?.has(request.url());
}

function reattributeEntryAssets(
  assetDocuments: Map<Request, number>,
  requiredAssets: Map<number, Set<string>>,
  documentStarts: Map<number, number>,
) {
  // The preload scanner can start entry resources before the commit event.
  // Reattribute only HTML-declared entries that started after this document's
  // response headers; outgoing speculative chunks retain their old generation.
  for (const [generation, urls] of requiredAssets) {
    for (const [request, owner] of assetDocuments) {
      if (
        owner === generation - 1 &&
        urls.has(request.url()) &&
        request.timing().startTime >= documentStarts.get(generation)!
      ) {
        assetDocuments.set(request, generation);
      }
    }
  }
}

const scope = {
  modules: [
    { tank: 1, module: 1 },
    { tank: 1, module: 2 },
  ],
};

test("diagram view fits, zooms below Fit, and keeps its view across system controls", async ({ page }) => {
  const dashboard = new ContainerStandalonePage(page);
  const writes: string[] = [];
  page.on("request", (request) => {
    if (/\/SetContainer(Control|Setpoint)$/.test(new URL(request.url()).pathname)) writes.push(request.url());
  });
  await dashboard.openOverview();
  const canvas = dashboard.diagramCanvas();
  const frame = await canvas.boundingBox();
  const content = await dashboard.schematic().getByTestId("system-diagram-content").boundingBox();
  expect(content!.x).toBeGreaterThanOrEqual(frame!.x);
  expect(content!.y).toBeGreaterThanOrEqual(frame!.y);
  expect(content!.x + content!.width).toBeLessThanOrEqual(frame!.x + frame!.width);
  expect(content!.y + content!.height).toBeLessThanOrEqual(frame!.y + frame!.height);
  await expect(dashboard.schematic().getByText(/Drag to pan/)).toBeVisible();
  const fitted = await dashboard.diagramZoom();
  await dashboard.zoomDiagram("out");
  await dashboard.fitDiagram();
  await expect.poll(() => dashboard.diagramZoom()).toBe(fitted);

  await canvas.press("+");
  await expect.poll(() => dashboard.diagramZoom()).toBeGreaterThan(fitted);
  await dashboard.zoomDiagramToReadableSize();
  const beforeDrag = await dashboard.diagramTransform();
  await dashboard.dragPump();
  await expect.poll(() => dashboard.diagramTransform()).not.toBe(beforeDrag);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  const inspected = await dashboard.diagramTransform();
  await dashboard.openEquipmentSettings("Coolant pump");
  await dashboard.closeEquipmentSettings("Coolant pump");
  expect(await dashboard.diagramTransform()).toBe(inspected);
  await canvas.focus();
  await canvas.press("0");
  await expect.poll(() => dashboard.diagramZoom()).toBe(fitted);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(writes).toEqual([]);
  await dashboard.schematic().screenshot({ path: test.info().outputPath("diagram-view.png") });
});

test("diagram pinch zoom keeps equipment closed and Fit restores the complete system", async ({ page }) => {
  const dashboard = new ContainerStandalonePage(page);
  await dashboard.openOverview();
  const fitted = await dashboard.diagramZoom();
  await dashboard.pinchDiagram();
  await expect.poll(() => dashboard.diagramZoom()).toBeGreaterThan(fitted * 2);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await dashboard.fitDiagram();
  await expect.poll(() => dashboard.diagramZoom()).toBe(fitted);
});

const systemTest = test.extend<{ systemBaseline: V2GetContainerControlsResponse }>({
  systemBaseline: [
    async ({ playwright, baseURL }, provide) => {
      const cleanup = await playwright.request.newContext({ baseURL, timeout: 10_000 });
      const headers = { "X-Frigate-Actor": "proto-os" };
      try {
        const response = await cleanup.get("/api/v2/power/container/controls");
        expect(response.ok()).toBe(true);
        const before = (await response.json()) as V2GetContainerControlsResponse;
        const names = [
          "coolant_pump_speed_hz",
          "dry_cooler_manual_speed_percent",
          "air_conditioner_1_temp_c",
          "air_conditioner_2_temp_c",
        ];
        const setpoints = before.setpoints!.filter((item) => names.includes(item.name!));
        const controls = before.controls!.filter((item) =>
          ["dry_cooler_auto", "coolant_pump", "air_conditioner_1", "air_conditioner_2", "tank_led_light"].includes(
            item.name!,
          ),
        );
        expect(setpoints).toHaveLength(4);
        expect(controls).toHaveLength(5);
        expect(controls.every((control) => control.stateKnown)).toBe(true);
        try {
          await provide(before);
        } finally {
          for (const setting of setpoints) {
            const restored = await cleanup.post(`/api/v2/power/container/setpoints/${setting.name}`, {
              headers,
              data: { value: setting.value },
            });
            expect(restored.ok()).toBe(true);
          }
          for (const control of controls) {
            const restored = await cleanup.post(`/api/v2/power/container/controls/${control.name}`, {
              headers,
              data: { enabled: control.enabled },
            });
            expect(restored.ok()).toBe(true);
          }
          const after = (await (
            await cleanup.get("/api/v2/power/container/controls")
          ).json()) as V2GetContainerControlsResponse;
          expect(after.setpoints!.filter((item) => names.includes(item.name!))).toEqual(setpoints);
          expect(after.controls!.filter((item) => controls.some((control) => control.name === item.name))).toEqual(
            controls,
          );
        }
      } finally {
        await cleanup.dispose();
      }
    },
    { auto: true, timeout: 30_000 },
  ],
});

systemTest(
  "schematic pump and dry-cooler settings round trip through native MDKv2",
  async ({ page, request, baseURL, systemBaseline }) => {
    expect(systemBaseline.supported).toBe(true);
    const dashboard = new ContainerStandalonePage(page);
    await dashboard.openOverview();
    await page.getByRole("region", { name: "Container cooling system schematic" }).scrollIntoViewIfNeeded();
    await page
      .getByRole("region", { name: "Container cooling system schematic" })
      .screenshot({ path: test.info().outputPath("system-overview.png") });
    await dashboard.openEquipmentSettings("Coolant pump");
    await expect(
      dashboard.equipmentSettings("Coolant pump").getByRole("button", { name: "Apply", exact: true }),
    ).toHaveCount(0);
    const cancelledRequests: Request[] = [];
    const captureRequest = (request: Request) => {
      cancelledRequests.push(request);
    };
    page.on("request", captureRequest);
    await dashboard.cancelEquipmentPower("Coolant pump", "Cancel");
    await dashboard.cancelEquipmentPower("Coolant pump", "Escape");
    expect(
      cancelledRequests.filter((request) => new URL(request.url()).pathname.endsWith("/SetContainerControl")),
    ).toHaveLength(0);
    page.off("request", captureRequest);
    const powerWrite = await dashboard.setEquipmentPower("Coolant pump", false);
    expect(powerWrite.ok()).toBe(true);
    expectDirectRequest(powerWrite.request(), baseURL!);
    expect(fromBinary(SetContainerControlRequestSchema, powerWrite.request().postDataBuffer()!)).toMatchObject({
      name: "coolant_pump",
      enabled: false,
    });
    await dashboard.setEquipmentPower("Coolant pump", true);
    await page.screenshot({ path: test.info().outputPath("pump-settings.png") });
    const pumpWrite = await dashboard.applyPumpSpeed(45);
    expect(pumpWrite.ok()).toBe(true);
    expectDirectRequest(pumpWrite.request(), baseURL!);
    expect(fromBinary(SetContainerSetpointRequestSchema, pumpWrite.request().postDataBuffer()!)).toMatchObject({
      name: "coolant_pump_speed_hz",
      value: 45,
    });
    await dashboard.closeEquipmentSettings("Coolant pump");
    await dashboard.openEquipmentSettings("Coolant pump");
    await expect(
      dashboard.equipmentSettings("Coolant pump").getByLabel("Pump speed (Hz)", { exact: true }),
    ).toHaveValue("45");
    await dashboard.closeEquipmentSettings("Coolant pump");

    await dashboard.openEquipmentSettings("Dry cooler");
    await expect(
      dashboard.equipmentSettings("Dry cooler").getByLabel("Target inlet temperature (°C)", { exact: true }),
    ).toBeVisible();
    const fanWrite = await dashboard.applyManualFanSpeed(65);
    expect(fanWrite.ok()).toBe(true);
    expectDirectRequest(fanWrite.request(), baseURL!);
    expect(fromBinary(SetContainerSetpointRequestSchema, fanWrite.request().postDataBuffer()!)).toMatchObject({
      name: "dry_cooler_manual_speed_percent",
      value: 65,
    });
    const settings = (await (
      await request.get("/api/v2/power/container/controls")
    ).json()) as V2GetContainerControlsResponse;
    expect(settings.controls!.find((item) => item.name === "dry_cooler_auto")).toMatchObject({
      enabled: false,
      stateKnown: true,
    });
    expect(settings.setpoints!.find((item) => item.name === "dry_cooler_manual_speed_percent")).toMatchObject({
      value: 65,
      unit: "%",
    });
    await dashboard.closeEquipmentSettings("Dry cooler");
    await dashboard.openEquipmentSettings("Dry cooler");
    await expect(dashboard.equipmentSettings("Dry cooler").getByLabel("Fan speed (%)", { exact: true })).toHaveValue(
      "65",
    );
    await page.screenshot({ path: test.info().outputPath("cooler-settings.png") });
  },
);

systemTest(
  "central controls expose air conditioning and lighting with responsive equipment groups",
  async ({ page, baseURL, systemBaseline }, testInfo) => {
    const dashboard = new ContainerStandalonePage(page);
    await dashboard.openOverview();
    await dashboard.openEquipmentSettings("Coolant pump");
    await dashboard.expectResponsiveControls(testInfo.project.name === "desktop");
    for (const [name, label] of [
      ["air_conditioner_1", "Air conditioner 1 power"],
      ["air_conditioner_2", "Air conditioner 2 power"],
      ["tank_led_light", "Tank lights"],
    ]) {
      const desired = !systemBaseline.controls!.find((item) => item.name === name)!.enabled;
      const response = await dashboard.setSystemControl(name, label, desired);
      expectDirectRequest(response.request(), baseURL!);
      expect(fromBinary(SetContainerControlRequestSchema, response.request().postDataBuffer()!)).toMatchObject({
        name,
        enabled: desired,
      });
    }
    for (const number of [1, 2] as const) {
      const desired =
        systemBaseline.setpoints!.find((item) => item.name === `air_conditioner_${number}_temp_c`)!.value! + 1;
      const response = await dashboard.applyAirConditionerTemperature(number, desired);
      expectDirectRequest(response.request(), baseURL!);
      expect(fromBinary(SetContainerSetpointRequestSchema, response.request().postDataBuffer()!)).toMatchObject({
        name: `air_conditioner_${number}_temp_c`,
        value: desired,
      });
    }
    await page.screenshot({ path: test.info().outputPath("central-system-controls.png") });
  },
);

systemTest("numeric blur preserves a single Mode click and waits for target readback", async ({ page }) => {
  const dashboard = new ContainerStandalonePage(page);
  await dashboard.openOverview();
  await dashboard.openEquipmentSettings("Dry cooler");
  await dashboard.coolerMode().click();
  await dashboard.coolerModeOption("Manual").click();
  await expect(page.getByTestId("toast").filter({ hasText: "Mode set to Manual." })).toBeVisible();
  await expect(dashboard.manualFanSpeed()).toBeEnabled();
  const desired = Number(await dashboard.manualFanSpeed().inputValue()) - 1;
  const setpoints: Request[] = [];
  const controls: Request[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/SetContainerSetpoint")) setpoints.push(request);
    if (path.endsWith("/SetContainerControl")) controls.push(request);
  });
  let releaseReadback!: () => void;
  const heldReadback = new Promise<void>((resolve) => {
    releaseReadback = resolve;
  });
  await page.route(
    "**/mdk.v2.PowerService/GetContainerControls",
    async (route) => {
      const response = await route.fetch();
      await heldReadback;
      await route.fulfill({ response });
    },
    { times: 1 },
  );
  try {
    await dashboard.manualFanSpeed().fill(String(desired));
    await dashboard.coolerMode().click();
    await expect(dashboard.coolerModeOption("Auto")).toBeVisible();
    await expect.poll(() => setpoints.length).toBe(1);
    await dashboard.coolerModeOption("Auto").click();
    expect(controls).toHaveLength(0);
  } finally {
    releaseReadback();
  }
  await expect(page.getByTestId("toast").filter({ hasText: "Mode set to Auto." })).toBeVisible();
  expect(controls).toHaveLength(1);
  expect(setpoints).toHaveLength(1);
  expect(fromBinary(SetContainerSetpointRequestSchema, setpoints[0].postDataBuffer()!)).toMatchObject({
    name: "dry_cooler_manual_speed_percent",
    value: desired,
  });
  expect(fromBinary(SetContainerControlRequestSchema, controls[0].postDataBuffer()!)).toMatchObject({
    name: "dry_cooler_auto",
    enabled: true,
  });
});

systemTest(
  "autosave reports rejected writes inline and in a toast, then retries on Enter",
  async ({ page, request }) => {
    const dashboard = new ContainerStandalonePage(page);
    await dashboard.openOverview();
    await dashboard.openEquipmentSettings("Coolant pump");
    const dialog = dashboard.equipmentSettings("Coolant pump");
    const input = dialog.getByLabel("Pump speed (Hz)", { exact: true });
    const original = await input.inputValue();
    const desired = Number(original) + 1;
    await page.route(
      "**/mdk.v2.PowerService/SetContainerSetpoint",
      (route) =>
        route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({ code: "internal", message: "Simulated command rejection" }),
        }),
      { times: 1 },
    );
    await input.fill(String(desired));
    await input.press("Enter");
    await expect(dialog.getByText(/Simulated command rejection/)).toBeVisible();
    await expect(page.getByTestId("toast").filter({ hasText: /Could not save pump speed/ })).toBeVisible();
    await expect(input).toHaveAttribute("aria-invalid", "true");
    await expect(input).toHaveValue(String(desired));
    const settings = (await (
      await request.get("/api/v2/power/container/controls")
    ).json()) as V2GetContainerControlsResponse;
    expect(settings.setpoints!.find((item) => item.name === "coolant_pump_speed_hz")!.value).toBe(Number(original));
    await expect(input).toBeEnabled();
    await input.press("Enter");
    await expect(page.getByTestId("toast").filter({ hasText: "Pump speed saved." })).toBeVisible();
    await expect(input).not.toHaveAttribute("aria-invalid", "true");
    await dashboard.closeEquipmentSettings("Coolant pump");
    await dashboard.openEquipmentSettings("Coolant pump");
    await expect(dialog.getByLabel("Pump speed (Hz)", { exact: true })).toHaveValue(String(desired));
  },
);

test("production bundle probes CT1 and survives tank/module deep-link refreshes without login", async ({
  page,
  baseURL,
}) => {
  const apiRequests: Request[] = [];
  const probes = new Set<Request>();
  const assets: Response[] = [];
  const failedAssets: Request[] = [];
  const assetDocuments = new Map<Request, number>();
  const requiredAssets = new Map<number, Set<string>>();
  const documentStarts = new Map<number, number>();
  const documentReads: Promise<void>[] = [];
  const errors: string[] = [];
  let documentGeneration = 0;
  let pendingDocument: Response | undefined;
  let awaitingProbe = true;
  const assetTypes = new Set(["script", "stylesheet", "font"]);
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (assetTypes.has(request.resourceType())) assetDocuments.set(request, documentGeneration);
    const pathname = new URL(request.url()).pathname;
    if (pathname.startsWith("/api/") || pathname.startsWith("/mdk.v2.")) {
      apiRequests.push(request);
      // Each full document load creates one resolver. Only its first info
      // request is a probe; later same-path adapter reads must carry the actor.
      if (pathname === "/api/v2/system/info" && awaitingProbe) {
        probes.add(request);
        awaitingProbe = false;
      }
    }
  });
  page.on("response", (response) => {
    if (assetTypes.has(response.request().resourceType())) assets.push(response);
    if (response.request().isNavigationRequest() && response.request().frame() === page.mainFrame()) {
      pendingDocument = response;
    }
  });
  page.on("framenavigated", (frame) => {
    // SPA/history changes have no document response and must not reset ownership.
    if (frame !== page.mainFrame() || !pendingDocument) return;
    const response = pendingDocument;
    pendingDocument = undefined;
    const generation = ++documentGeneration;
    awaitingProbe = true;
    const timing = response.request().timing();
    documentStarts.set(generation, timing.startTime + timing.responseStart);
    documentReads.push(
      response.text().then((html) => {
        // Parse only Vite's emitted entry tags, not speculative links inserted
        // into the live DOM by route prefetching after boot.
        const tags = html.match(/<(?:script|link)\b[^>]*>/g) ?? [];
        const urls = tags
          .filter((tag) => tag.startsWith("<script") || /\brel=["'](?:stylesheet|modulepreload)["']/.test(tag))
          .flatMap((tag) => {
            const match = /\b(?:src|href)=["']([^"']+)["']/.exec(tag);
            return match ? [new URL(match[1], response.url()).href] : [];
          });
        requiredAssets.set(generation, new Set(urls));
      }),
    );
  });
  page.on("requestfailed", (request) => {
    if (assetTypes.has(request.resourceType())) failedAssets.push(request);
  });
  const dashboard = new ContainerStandalonePage(page);
  const probe = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v2/system/info");
  await dashboard.openOverview();
  const info = await (await probe).json();
  expect(info.info.product).toBe("CT1");
  await dashboard.expectNoLogin();
  expect(apiRequests.some((request) => new URL(request.url()).pathname.endsWith("/QueryTimeSeries"))).toBe(false);
  await dashboard.openHashrate();

  await dashboard.openTank(1);
  await expect(dashboard.tank(1).getByRole("button", { name: "Module 01, Healthy", exact: true })).toBeVisible();
  await page.reload();
  await expect(dashboard.power(1)).toBeEnabled();
  await expect(dashboard.power(1)).toBeChecked();
  await dashboard.expectNoLogin();

  await dashboard.openModule();
  await dashboard.openModule(true);
  await dashboard.expectNoLogin();
  await page.getByRole("dialog", { name: "Modules", exact: true }).getByRole("button", { name: "Done" }).click();
  await expect(page).toHaveURL(/\/tanks\/1$/);

  expect(probes.size).toBeGreaterThan(0);
  for (const request of probes) {
    expect(new URL(request.url()).origin).toBe(new URL(baseURL!).origin);
    expect(request.headers().authorization).toBeUndefined();
    expect(request.headers()["x-frigate-actor"]).toBeUndefined();
  }
  const adapterRequests = apiRequests.filter((request) => !probes.has(request));
  expect(
    adapterRequests.some((request) => new URL(request.url()).pathname === "/mdk.v2.SystemService/GetSystemInfo"),
  ).toBe(true);
  for (const request of adapterRequests) {
    expect(new URL(request.url()).pathname).toMatch(/^\/mdk\.v2\./);
    expectDirectRequest(request, baseURL!);
  }
  expect(
    apiRequests.some((request) => new URL(request.url()).pathname === "/mdk.v2.HashboardService/GetContainerMap"),
  ).toBe(true);

  // Force both fonts used by Home/tanks to load: CSS failing entirely would
  // otherwise make document.fonts.ready succeed with an empty font set.
  const fontsLoaded = await page.evaluate(async () => {
    const [body, mono] = await Promise.all([
      document.fonts.load('16px "Inter"'),
      document.fonts.load('16px "JetBrainsMono"'),
    ]);
    await document.fonts.ready;
    return (
      body.length > 0 &&
      mono.length > 0 &&
      body.every((font) => font.status === "loaded") &&
      mono.every((font) => font.status === "loaded")
    );
  });
  expect(fontsLoaded).toBe(true);
  for (const type of assetTypes) {
    expect(assets.some((response) => response.request().resourceType() === type)).toBe(true);
  }
  await Promise.all(documentReads);
  reattributeEntryAssets(assetDocuments, requiredAssets, documentStarts);
  expect(requiredAssets.size).toBe(documentGeneration);
  for (const [generation, urls] of requiredAssets) {
    expect(urls.size).toBeGreaterThan(0);
    for (const url of urls) {
      const response = assets.find(
        (response) => assetDocuments.get(response.request()) === generation && response.url() === url,
      );
      expect(response, `Missing required asset ${url} for document ${generation}`).toBeDefined();
      expect(await response!.finished()).toBeNull();
      expect(response!.ok()).toBe(true);
    }
  }
  const supersededOptional = (request: Request) =>
    isSupersededOptionalAsset(request, assetDocuments, requiredAssets, documentGeneration);
  // Chromium can omit the terminal event for an outgoing speculative load,
  // leaving finished() pending even after navigation has replaced its document.
  // Entry assets above and all current-document assets must still complete.
  for (const response of assets.filter((response) => !supersededOptional(response.request()))) {
    expect(await response.finished()).toBeNull();
  }
  for (const response of assets) {
    expect(response.ok()).toBe(true);
    const url = new URL(response.url());
    expect(url.origin).toBe(new URL(baseURL!).origin);
    expect(response.headers()["content-type"]).not.toContain("text/html");
  }
  // Background route prefetches from an old document may be intentionally
  // aborted by navigation. Never ignore HTTP errors or failures in this document.
  expect(failedAssets.filter((request) => !supersededOptional(request)).map((request) => request.url())).toEqual([]);
  for (const request of failedAssets) expect(request.failure()?.errorText).toBe("net::ERR_ABORTED");
  const stylesheets = assets.filter((response) => response.request().resourceType() === "stylesheet");
  for (const response of stylesheets) {
    expect(new URL(response.url()).pathname).toMatch(/^\/assets\/.+\.css$/);
    expect(response.headers()["content-type"]).toContain("text/css");
  }
  const scripts = assets.filter((response) => response.request().resourceType() === "script");
  for (const response of scripts) expect(new URL(response.url()).pathname).toMatch(/^\/assets\/.+\.js$/);
  const fontPaths = assets
    .filter((response) => response.request().resourceType() === "font")
    .map((response) => decodeURIComponent(new URL(response.url()).pathname));
  expect(fontPaths).toContain("/fonts/Inter/InterVariable.woff2");
  expect(fontPaths).toContain("/fonts/JetBrainsMono/JetBrainsMono[wght].ttf");
  expect(errors).toEqual([]);
});

const scopedTest = test.extend<{ tankBaseline: Awaited<ReturnType<typeof miningStates>> }>({
  tankBaseline: [
    async ({ playwright, baseURL }, provide) => {
      // Fixture teardown owns a separate deadline and request context, even
      // when the browser scenario consumes its entire test timeout.
      const cleanup = await playwright.request.newContext({ baseURL, timeout: 10_000 });
      try {
        const before = await miningStates(cleanup);
        try {
          await provide(before);
        } finally {
          const restored = await cleanup.post("/api/v2/mining/scoped/start", {
            headers: { "X-Frigate-Actor": "proto-os" },
            data: { scope },
          });
          expect(restored.ok()).toBe(true);
          expect(await miningStates(cleanup)).toEqual(before);
        }
      } finally {
        await cleanup.dispose();
      }
    },
    { timeout: 30_000 },
  ],
});

scopedTest(
  "actor-only scoped tank stop/start changes visible state and leaves the other tank untouched",
  async ({ page, request, baseURL, tankBaseline: before }) => {
    // Cover returning users separately: the boot/deep-link scenario above uses
    // genuinely empty storage so valid v1 credentials cannot mask a login gate.
    await page.addInitScript(() => {
      if (localStorage.getItem("proto-os-auth")) return;
      localStorage.setItem(
        "proto-os-auth",
        JSON.stringify({
          state: {
            auth: {
              session: {
                mode: "bearer",
                status: "authenticated",
                accessToken: { value: "stored-v1-access-token", expiresAtMs: Date.now() + 3_600_000 },
                refreshToken: { value: "stored-v1-refresh-token", expiresAtMs: Date.now() + 86_400_000 },
              },
            },
          },
          version: 0,
        }),
      );
    });
    const browserReads: Request[] = [];
    let initialProbe: Request | undefined;
    page.on("request", (request) => {
      const pathname = new URL(request.url()).pathname;
      if (
        !(request.method() === "GET" && pathname.startsWith("/api/")) &&
        !(request.method() === "POST" && pathname.startsWith("/mdk.v2."))
      )
        return;
      browserReads.push(request);
      if (pathname === "/api/v2/system/info" && !initialProbe) initialProbe = request;
    });
    const dashboard = new ContainerStandalonePage(page);
    expect(before).toHaveLength(12);
    expect(before.every((board) => board.state === "MINING_STATE_MINING")).toBe(true);
    await dashboard.openTank(1);
    await expect(dashboard.power(1)).toBeChecked();

    await test.step("stop only Tank 1", async () => {
      const response = page.waitForResponse(
        (response) => new URL(response.url()).pathname === "/mdk.v2.MiningService/StopMiningScoped",
      );
      await dashboard.setTankPower(1, false);
      const stopped = await response;
      expect(stopped.ok()).toBe(true);
      expectDirectRequest(stopped.request(), baseURL!);
      await expect(dashboard.power(1)).not.toBeChecked();
      await expect(dashboard.tank(1).getByRole("button", { name: "Module 01, Sleeping", exact: true })).toBeVisible();
      await expect(dashboard.power(2)).toBeChecked();
      const after = await miningStates(request);
      expect(after.filter((board) => board.tank === 1).every((board) => board.state === "MINING_STATE_IDLE")).toBe(
        true,
      );
      expect(after.filter((board) => board.tank === 2)).toEqual(before.filter((board) => board.tank === 2));
    });

    await test.step("start Tank 1 again", async () => {
      const response = page.waitForResponse(
        (response) => new URL(response.url()).pathname === "/mdk.v2.MiningService/StartMiningScoped",
      );
      await dashboard.setTankPower(1, true);
      const started = await response;
      expect(started.ok()).toBe(true);
      expectDirectRequest(started.request(), baseURL!);
      await expect(dashboard.power(1)).toBeChecked();
      await expect(dashboard.tank(1).getByRole("button", { name: "Module 01, Healthy", exact: true })).toBeVisible();
      expect(await miningStates(request)).toEqual(before);
      await dashboard.expectNoLogin();
    });

    expect(initialProbe).toBeDefined();
    expect(new URL(initialProbe!.url()).origin).toBe(new URL(baseURL!).origin);
    expect(initialProbe!.headers().authorization).toBeUndefined();
    expect(initialProbe!.headers()["x-frigate-actor"]).toBeUndefined();
    const adapterReads = browserReads.filter((request) => request !== initialProbe);
    expect(
      adapterReads.some((request) => new URL(request.url()).pathname === "/mdk.v2.SystemService/GetSystemInfo"),
    ).toBe(true);
    expect(
      adapterReads.some((request) => new URL(request.url()).pathname === "/mdk.v2.HashboardService/GetContainerMap"),
    ).toBe(true);
    for (const request of adapterReads) {
      expect(new URL(request.url()).pathname).toMatch(/^\/mdk\.v2\./);
      expectDirectRequest(request, baseURL!);
    }
  },
);
