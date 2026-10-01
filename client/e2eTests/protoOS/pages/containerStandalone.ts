import { expect, type Page } from "@playwright/test";
import type {
  V2GetContainerMapResponse,
  V2GetHashboardASICsResponse,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";

export class ContainerStandalonePage {
  constructor(private page: Page) {}

  async openOverview() {
    const historyRead = this.page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === "GET" &&
        url.pathname === "/api/v2/telemetry/timeseries" &&
        url.searchParams.get("metric") === "TIME_SERIES_METRIC_HASHRATE" &&
        !url.searchParams.has("identifier")
      );
    });
    await this.page.goto("/");
    await expect(this.page.getByRole("navigation", { name: "Container Home" })).toBeVisible();
    await expect(this.page.getByRole("link", { name: "View Hashrate" })).toBeVisible();
    const response = await historyRead;
    expect(response.ok()).toBe(true);
    const data = await response.json();
    expect(data.metric).toBe("TIME_SERIES_METRIC_HASHRATE");
    expect(data.unit).toBe("TH/s");
    expect(data.buckets.length).toBeGreaterThan(0);
    const populated = data.buckets.some(
      (bucket: { time: string; avg: number; count: string }) =>
        Number.isFinite(Date.parse(bucket.time)) &&
        Number.isFinite(bucket.avg) &&
        bucket.avg > 0 &&
        Number(bucket.count) > 0,
    );
    expect(populated).toBe(true);
    const chart = this.page.getByRole("figure", {
      name: "History in TH/s for the selected period. Latest values are listed alongside the chart.",
      exact: true,
    });
    await expect(chart).toBeVisible();
    await expect(chart.locator(".recharts-line-curve")).toHaveCount(1);
    await expect(chart.locator(".recharts-line-curve")).toHaveAttribute("d", /\S+/);
    await expect(this.page.getByText(/^[\d,]+\.\d TH\/s$/)).toBeVisible();
    await expect(this.page.getByText("12/12 hashboards hashing", { exact: true })).toBeVisible();
  }

  async openTank(tank: number) {
    await this.page.goto(`/tanks/${tank}`);
    await expect(this.tank(tank)).toBeVisible();
    await expect(this.power(tank)).toBeEnabled();
  }

  tank(tank: number) {
    return this.page.getByRole("region", { name: `Tank ${tank}`, exact: true });
  }

  power(tank: number) {
    return this.tank(tank).getByRole("checkbox", { name: `Tank ${tank} power`, exact: true });
  }

  async setTankPower(tank: number, on: boolean) {
    await this.power(tank).locator("..").click();
    const action = on ? "Turn on" : "Turn off";
    await expect(this.page.getByText(`${action} Tank ${tank}?`, { exact: true })).toBeVisible();
    await this.page.getByRole("button", { name: action, exact: true }).click();
  }

  async openModule(reload = false) {
    const mapResponse = await this.page.request.get("/api/v2/container/map");
    expect(mapResponse.ok()).toBe(true);
    const map = (await mapResponse.json()) as V2GetContainerMapResponse;
    const module = map.containerMap?.tanks?.find((tank) => tank.id === 1)?.modules?.find((module) => module.id === 1);
    expect(module).toBeDefined();
    const boards = (module!.slots ?? []).flatMap(({ hashboard }) => (hashboard ? [hashboard] : []));
    expect(boards).toHaveLength(3);
    const targets = boards.map((board) => {
      expect(board.macAddress).toBeTruthy();
      const slot = Number(board.location?.hashboardPosition);
      expect([1, 2, 3]).toContain(slot);
      return { identifier: board.macAddress!, slot };
    });
    expect(new Set(targets.map((target) => target.identifier)).size).toBe(3);
    expect(new Set(targets.map((target) => target.slot)).size).toBe(3);
    const reads = targets.map(({ identifier }) =>
      this.page.waitForResponse(
        (response) => decodeURIComponent(new URL(response.url()).pathname) === `/api/v2/hashboards/${identifier}/asics`,
      ),
    );
    const dialog = this.page.getByRole("dialog", { name: "Modules", exact: true });
    const [responses] = await Promise.all([
      Promise.all(reads),
      (async () => {
        if (reload) await this.page.reload();
        else await this.page.goto("/tanks/1/modules/1");
        await expect(dialog).toBeVisible();
        await expect(dialog.getByRole("heading", { name: "Hashboards", exact: true })).toBeVisible();
      })(),
    ]);
    for (const [index, response] of responses.entries()) {
      expect(response.ok()).toBe(true);
      const data = (await response.json()) as V2GetHashboardASICsResponse;
      const position = data.layout?.entries?.find((entry) => entry.x === 0 && entry.y === 0);
      expect(position?.index).toBeDefined();
      const asic = data.asics?.find((asic) => asic.index === position!.index);
      expect(asic?.temperatureDc).toBeGreaterThan(0);
      const board = dialog
        .getByTestId("card")
        .filter({ has: this.page.getByText(`Board ${targets[index].slot}`, { exact: true }) });
      const cell = board.getByTestId("asic-0-0");
      await expect(cell).toBeVisible();
      // Polling may replace the first response's sample before the assertion.
      // Require a populated numeric reading, not equality to a stale sample.
      await expect.poll(async () => Number(await cell.getAttribute("data-value"))).toBeGreaterThan(0);
    }
    await expect(dialog.getByRole("alert")).toHaveCount(0);
  }

  async expectNoLogin() {
    await expect(this.page.getByTestId("login-button")).toHaveCount(0);
    await expect(this.page.getByTestId("password")).toHaveCount(0);
    await expect(this.page).not.toHaveURL(/\/(login|onboarding)(\/|$)/);
  }
}
