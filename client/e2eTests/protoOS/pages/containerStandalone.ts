import { type DescMessage, fromBinary, type MessageShape } from "@bufbuild/protobuf";
import { expect, type Page, type Response } from "@playwright/test";
import type { V2GetContainerMapResponse } from "../../../src/protoOS/api/adapters/mdkV2/generated/generatedApi";
import { TimeSeriesMetric } from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/common_pb";
import {
  GetHashboardASICsRequestSchema,
  GetHashboardASICsResponseSchema,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/hashboard_pb";
import {
  SetContainerControlRequestSchema,
  SetContainerSetpointRequestSchema,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/power_pb";
import {
  QueryTimeSeriesRequestSchema,
  QueryTimeSeriesResponseSchema,
} from "../../../src/protoOS/api/adapters/mdkV2/generated/proto/mdk/v2/telemetry_pb";

const connectRequest = <Desc extends DescMessage>(
  response: Response,
  procedure: string,
  schema: Desc,
): MessageShape<Desc> | undefined => {
  const request = response.request();
  if (request.method() !== "POST" || new URL(response.url()).pathname !== procedure) return undefined;
  return fromBinary(schema, request.postDataBuffer() ?? new Uint8Array());
};

export class ContainerStandalonePage {
  constructor(private page: Page) {}

  async openSystem() {
    await this.page.goto("/?view=system");
    await expect(this.page.getByRole("navigation", { name: "Container Home" })).toBeVisible();
    await expect(this.page.getByRole("region", { name: "Container cooling system schematic" })).toBeVisible();
    const readings = this.page.getByRole("region", { name: "Live readings" });
    await expect(readings.getByRole("heading", { name: "Container power", exact: true })).toBeVisible();
    await expect(readings.getByText(/^[\d,]+\.\d kW$/)).toBeVisible();
    await expect(this.page.getByRole("figure")).toHaveCount(0);
    await expect(this.page.getByLabel("Diagram zoom level", { exact: true })).toHaveText(/^\d+%$/);
  }

  schematic() {
    return this.page.getByRole("region", { name: "Container cooling system schematic", exact: true });
  }

  diagramCanvas() {
    return this.schematic().getByRole("group", { name: "Diagram canvas", exact: true });
  }

  async diagramZoom() {
    return Number.parseFloat((await this.schematic().getByLabel("Diagram zoom level", { exact: true }).textContent())!);
  }

  async zoomDiagram(direction: "in" | "out") {
    const before = await this.diagramZoom();
    await this.schematic()
      .getByRole("button", { name: `Zoom ${direction}`, exact: true })
      .click();
    if (direction === "in") await expect.poll(() => this.diagramZoom()).toBeGreaterThan(before);
    else await expect.poll(() => this.diagramZoom()).toBeLessThan(before);
  }

  async fitDiagram() {
    await this.schematic().getByRole("button", { name: "Fit whole diagram", exact: true }).click();
  }

  async zoomDiagramToReadableSize() {
    for (let i = 0; i < 8 && (await this.diagramZoom()) < 100; i++) await this.zoomDiagram("in");
    expect(await this.diagramZoom()).toBeGreaterThanOrEqual(100);
  }

  async diagramTransform() {
    return this.schematic().getByTestId("system-diagram-content").getAttribute("style");
  }

  async dragPump() {
    const box = await this.schematic()
      .getByRole("img", { name: /^Pump speed:/ })
      .boundingBox();
    expect(box).not.toBeNull();
    const start = { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 };
    await this.page.mouse.move(start.x, start.y);
    await this.page.mouse.down();
    await this.page.mouse.move(start.x - 80, start.y - 60, { steps: 10 });
    await this.page.mouse.up();
  }

  async pinchDiagram() {
    const box = await this.diagramCanvas().boundingBox();
    expect(box).not.toBeNull();
    const client = await this.page.context().newCDPSession(this.page);
    try {
      const center = { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 };
      const points = (spread: number) => [
        { x: center.x - spread, y: center.y, id: 1 },
        { x: center.x + spread, y: center.y, id: 2 },
      ];
      await client.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: points(35) });
      for (let i = 1; i <= 6; i++)
        await client.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: points(35 + i * 12) });
      await client.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
    } finally {
      await client.detach();
    }
  }

  async openHashrate() {
    const historyRead = this.page.waitForResponse((response) => {
      const query = connectRequest(response, "/mdk.v2.TelemetryService/QueryTimeSeries", QueryTimeSeriesRequestSchema);
      return query?.metric === TimeSeriesMetric.HASHRATE && query.identifier === "";
    });
    await this.page.getByRole("link", { name: "Hashrate", exact: true }).click();
    const response = await historyRead;
    expect(response.ok()).toBe(true);
    const data = fromBinary(QueryTimeSeriesResponseSchema, await response.body());
    expect(data.metric).toBe(TimeSeriesMetric.HASHRATE);
    expect(data.unit).toBe("TH/s");
    expect(data.buckets.length).toBeGreaterThan(0);
    const populated = data.buckets.some(
      (bucket) => bucket.time !== undefined && Number.isFinite(bucket.avg) && bucket.avg > 0 && bucket.count > 0n,
    );
    expect(populated).toBe(true);
    const chart = this.page.getByRole("figure", {
      name: "History in TH/s for the selected period. Latest values are listed alongside the chart.",
      exact: true,
    });
    await expect(chart).toBeVisible();
    await expect(chart.locator(".recharts-line-curve")).toHaveCount(1);
    await expect(chart.locator(".recharts-line-curve")).toHaveAttribute("d", /\S+/);
    const readings = this.page.getByRole("region", { name: "Live readings" });
    await expect(readings.getByText(/^[\d,]+\.\d TH\/s$/)).toBeVisible();
    await expect(readings.getByText("12/12 hashboards hashing", { exact: true })).toBeVisible();
  }

  async openTank(tank: number) {
    await this.page.goto(`/tanks/${tank}`);
    await expect(this.tank(tank)).toBeVisible();
    await expect(this.power(tank)).toBeEnabled();
  }

  systemControls() {
    return this.page.getByRole("dialog", { name: "System controls", exact: true });
  }

  equipmentSettings(equipment: "Coolant pump" | "Dry cooler" | "Air conditioner 1" | "Air conditioner 2") {
    return this.systemControls().getByRole("region", { name: `${equipment} controls`, exact: true });
  }

  async openEquipmentSettings(equipment: "Coolant pump" | "Dry cooler") {
    await this.page.getByRole("button", { name: "System controls", exact: true }).click();
    await expect(
      this.equipmentSettings(equipment).getByRole("checkbox", { name: `${equipment} power`, exact: true }),
    ).toBeEnabled();
    await expect(this.systemControls().locator("..")).toHaveCSS("opacity", "1");
    await expect(this.systemControls().getByTestId("modal")).toHaveCSS("opacity", "1");
  }

  async closeEquipmentSettings(equipment: "Coolant pump" | "Dry cooler") {
    await this.systemControls().getByRole("button", { name: "Close dialog", exact: true }).click();
    await expect(this.equipmentSettings(equipment)).toBeHidden();
  }

  async expectResponsiveControls(sideBySide: boolean) {
    const pump = await this.equipmentSettings("Coolant pump").boundingBox();
    const cooler = await this.equipmentSettings("Dry cooler").boundingBox();
    if (sideBySide) {
      expect(Math.abs(pump!.y - cooler!.y)).toBeLessThan(1);
      expect(cooler!.x).toBeGreaterThan(pump!.x + pump!.width);
    } else {
      expect(cooler!.y).toBeGreaterThan(pump!.y + pump!.height);
    }
    expect(await this.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  }

  async setEquipmentPower(equipment: "Coolant pump" | "Dry cooler", enabled: boolean) {
    const name = equipment === "Coolant pump" ? "coolant_pump" : "dry_cooler";
    return this.setSystemControl(name, `${equipment} power`, enabled);
  }

  powerConfirmation(label: string, enabled: boolean) {
    const equipment = label.replace(/ power$/, "");
    return this.page.getByRole("dialog", { name: `${enabled ? "Turn on" : "Turn off"} ${equipment}?`, exact: true });
  }

  async cancelEquipmentPower(equipment: "Coolant pump" | "Dry cooler", dismiss: "Cancel" | "Escape") {
    const power = this.equipmentSettings(equipment).getByRole("checkbox", { name: `${equipment} power`, exact: true });
    const enabled = await power.isChecked();
    await power.locator("..").click();
    const confirmation = this.powerConfirmation(`${equipment} power`, !enabled);
    await expect(confirmation).toBeVisible();
    if (dismiss === "Escape") await this.page.keyboard.press("Escape");
    else await confirmation.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(confirmation).toBeHidden();
    await expect(this.systemControls()).toBeVisible();
    await expect(power).toBeChecked({ checked: enabled });
  }

  async setSystemControl(name: string, label: string, enabled: boolean) {
    const written = this.page.waitForResponse(
      (response) =>
        connectRequest(response, "/mdk.v2.PowerService/SetContainerControl", SetContainerControlRequestSchema)?.name ===
        name,
    );
    const power = this.systemControls().getByRole("checkbox", { name: label, exact: true });
    await power.locator("..").click();
    await this.powerConfirmation(label, enabled)
      .getByRole("button", { name: enabled ? "Turn on" : "Turn off", exact: true })
      .click();
    const response = await written;
    await expect(power).toBeChecked({ checked: enabled });
    await expect(power).toBeEnabled();
    await expect(
      this.page.getByTestId("toast").filter({ hasText: `${label} turned ${enabled ? "on" : "off"}.` }),
    ).toBeVisible();
    return response;
  }

  async applyAirConditionerTemperature(number: 1 | 2, temperature: number) {
    const equipment = `Air conditioner ${number}` as const;
    await this.equipmentSettings(equipment)
      .getByLabel("Target temperature (°C)", { exact: true })
      .fill(String(temperature));
    return this.applyEquipmentSetpoint(
      equipment,
      `air_conditioner_${number}_temp_c`,
      "Target temperature (°C)",
      "Enter",
    );
  }

  async applyPumpSpeed(speed: number) {
    const input = this.equipmentSettings("Coolant pump").getByLabel("Pump speed (Hz)", { exact: true });
    await input.fill(String(speed));
    return this.applyEquipmentSetpoint("Coolant pump", "coolant_pump_speed_hz", "Pump speed (Hz)", "Enter");
  }

  coolerMode() {
    return this.equipmentSettings("Dry cooler").getByRole("button", { name: "Mode", exact: true });
  }

  coolerModeOption(mode: "Auto" | "Manual") {
    return this.page.getByRole("option", { name: mode, exact: true });
  }

  manualFanSpeed() {
    return this.equipmentSettings("Dry cooler").getByLabel("Fan speed (%)", { exact: true });
  }

  async applyManualFanSpeed(percent: number) {
    await this.coolerMode().click();
    await this.coolerModeOption("Manual").click();
    await expect(this.manualFanSpeed()).toBeEnabled();
    await expect(this.page.getByTestId("toast").filter({ hasText: "Mode set to Manual." })).toBeVisible();
    await this.manualFanSpeed().fill(String(percent));
    return this.applyEquipmentSetpoint("Dry cooler", "dry_cooler_manual_speed_percent", "Fan speed (%)", "blur");
  }

  private async applyEquipmentSetpoint(
    equipment: "Coolant pump" | "Dry cooler" | "Air conditioner 1" | "Air conditioner 2",
    name: string,
    label: string,
    commit: "Enter" | "blur",
  ) {
    const written = this.page.waitForResponse(
      (response) =>
        connectRequest(response, "/mdk.v2.PowerService/SetContainerSetpoint", SetContainerSetpointRequestSchema)
          ?.name === name,
    );
    const dialog = this.equipmentSettings(equipment);
    const input = dialog.getByLabel(label, { exact: true });
    if (commit === "Enter") await input.press("Enter");
    else await input.blur();
    const response = await written;
    await expect(dialog).toBeVisible();
    await expect(input).toBeEnabled();
    const savedLabel = equipment.startsWith("Air conditioner")
      ? `${equipment} target temperature`
      : label.split(" (")[0];
    await expect(this.page.getByTestId("toast").filter({ hasText: `${savedLabel} saved.` })).toBeVisible();
    return response;
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
        (response) =>
          connectRequest(response, "/mdk.v2.HashboardService/GetHashboardASICs", GetHashboardASICsRequestSchema)
            ?.identifier === identifier,
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
      const data = fromBinary(GetHashboardASICsResponseSchema, await response.body());
      const position = data.layout?.entries.find((entry) => entry.x === 0 && entry.y === 0);
      expect(position).toBeDefined();
      const asic = data.asics.find((asic) => asic.index === position!.index);
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
