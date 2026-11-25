import { expect, test } from "@playwright/test";
import type { V2GetContainerMapResponse } from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";

test.describe("Container tank health", () => {
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(!process.env.CT1_BASE_URL, "Requires a ProtoOS server pointed at fake-ct1 (CT1_BASE_URL)");
  test.use({ baseURL: process.env.CT1_BASE_URL });

  test("shows sleeping and offline modules without treating disabled rails as faults", async ({ page, request }) => {
    const response = await request.get("/api/v2/container/map");
    expect(response.ok()).toBe(true);
    const map = (await response.json()) as V2GetContainerMapResponse;
    const tank = map.containerMap?.tanks?.find((tank) => tank.id === 4);
    for (const [moduleNumber, state] of [
      [4, "MINING_STATE_IDLE"],
      [9, "MINING_STATE_DISCONNECTED"],
    ] as const) {
      const boards = tank?.modules?.find((module) => module.id === moduleNumber)?.slots?.map((slot) => slot.hashboard);
      expect(boards).toHaveLength(3);
      for (const board of boards ?? []) {
        expect(board?.miningState).toBe(state);
        expect(board?.health?.hashrailOk).toBe(false);
        expect(board?.health?.refclksOk).toBe(false);
        expect(board?.health?.errors ?? []).toEqual([]);
      }
    }

    await page.goto("/");
    const tank4 = page.getByRole("region", { name: "Tank 4", exact: true });
    await expect(tank4.getByRole("button", { name: "Tank 4 module 4, Sleeping", exact: true })).toBeVisible();
    await expect(tank4.getByRole("button", { name: "Tank 4 module 9, Offline", exact: true })).toBeVisible();
    await expect(tank4.getByRole("button", { name: /Needs attention/ })).toHaveCount(0);
    await expect(tank4.getByRole("button", { name: /Hashing$/ })).toHaveCount(14);

    const tank2 = page.getByRole("region", { name: "Tank 2", exact: true });
    await expect(tank2.getByRole("button", { name: "Tank 2 module 3, Sleeping", exact: true })).toBeVisible();
    await expect(tank2.getByRole("button", { name: "Tank 2 module 6, Offline", exact: true })).toBeVisible();
    await expect(tank2.getByRole("button", { name: "Tank 2 module 11, Needs attention", exact: true })).toBeVisible();
    await expect(
      page
        .getByRole("region", { name: "Tank 3", exact: true })
        .getByRole("button", { name: "Tank 3 module 8, Needs attention", exact: true }),
    ).toBeVisible();
  });
});
