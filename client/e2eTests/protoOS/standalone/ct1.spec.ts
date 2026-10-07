import { type APIRequestContext, expect, type Request, type Response, test } from "@playwright/test";
import type { V2GetContainerMapResponse } from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";
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
    if (pathname.startsWith("/api/")) {
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
  expect(adapterRequests.some((request) => new URL(request.url()).pathname === "/api/v2/system/info")).toBe(true);
  for (const request of adapterRequests) {
    expect(new URL(request.url()).pathname).toMatch(/^\/api\/v2\//);
    expectDirectRequest(request, baseURL!);
  }
  expect(apiRequests.some((request) => new URL(request.url()).pathname === "/api/v2/container/map")).toBe(true);

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
      if (request.method() !== "GET" || !pathname.startsWith("/api/")) return;
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
        (response) => new URL(response.url()).pathname === "/api/v2/mining/scoped/stop",
      );
      await dashboard.setTankPower(1, false);
      const stopped = await response;
      expect(stopped.ok()).toBe(true);
      expectDirectRequest(stopped.request(), baseURL!);
      expect(stopped.request().postDataJSON()).toEqual({ scope });
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
        (response) => new URL(response.url()).pathname === "/api/v2/mining/scoped/start",
      );
      await dashboard.setTankPower(1, true);
      const started = await response;
      expect(started.ok()).toBe(true);
      expectDirectRequest(started.request(), baseURL!);
      expect(started.request().postDataJSON()).toEqual({ scope });
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
    expect(adapterReads.some((request) => new URL(request.url()).pathname === "/api/v2/system/info")).toBe(true);
    expect(adapterReads.some((request) => new URL(request.url()).pathname === "/api/v2/container/map")).toBe(true);
    for (const request of adapterReads) {
      expect(new URL(request.url()).pathname).toMatch(/^\/api\/v2\//);
      expectDirectRequest(request, baseURL!);
    }
  },
);
