import { describe, expect, it } from "vitest";
import {
  toCanonicalCoolingStatus,
  toCanonicalErrors,
  toCanonicalHardware,
  toCanonicalMiningStatus,
  toCanonicalMiningTarget,
  toCanonicalNetworkInfo,
  toCanonicalPools,
  toCanonicalSystemInfo,
  toCanonicalSystemTag,
  toCanonicalTelemetry,
  toCanonicalTimeSeries,
  toV1CoolingMode,
} from "./normalize";
import { MetricUnit, PerformanceMode } from "@/protoOS/api/generatedApi";
import { getAsicId, getPsuId } from "@/protoOS/store";
import { asHashboardId, ROOT_MODULE_KEY } from "@/protoOS/store/contract";

describe("MDK v1 normalization", () => {
  it("maps a CU model to the container product family and CT1 module profile", () => {
    const systemInfo = toCanonicalSystemInfo({
      product_name: "Proto CU1",
      model: "CU1",
      cb_sn: "controller-1",
      os: { name: "ProtoOS", version: "1.8.2" },
    });

    expect(systemInfo.identity.deviceType).toBe("container");
    expect(systemInfo.moduleModel).toBe("ct1");
    expect(systemInfo.identity.serialNumber).toBe("controller-1");
  });

  it("builds a deterministic synthetic R2 module and collision-safe component ids", () => {
    const hardware = toCanonicalHardware(
      {
        "cb-info": { serial_number: "controller-1" },
        "hashboards-info": [
          { hb_sn: "HB-1", slot: 1, mining_asic_count: 2 },
          { hb_sn: "HB-2", slot: 2, mining_asic_count: 2 },
        ],
        "psus-info": [{ psu_sn: "PSU-1", slot: 1 }],
        "fans-info": [{ slot: 1, name: "Fan 1" }],
      },
      { deviceId: "device:controller-1", profile: "r2" },
    );

    expect(hardware.topology.kind).toBe("rig");
    expect(hardware.topology.modules.size).toBe(1);
    expect([...hardware.hashboards.keys()]).toEqual(["serial:HB-1", "serial:HB-2"]);
    expect([...hardware.psus.keys()]).toEqual(["module:root:psu:1"]);
    expect([...hardware.fans.keys()]).toEqual(["module:root:fan:1"]);
  });

  it("converts v1 latest and historical telemetry into timestamped canonical measurements", () => {
    const latest = toCanonicalTelemetry({
      miner: {
        hashrate: { value: 100, unit: MetricUnit.THS },
        efficiency: { value: 30, unit: MetricUnit.JTH },
        power: { value: 3_000, unit: MetricUnit.W },
        temperature: { value: 65, unit: MetricUnit.ValueC },
      },
      hashboards: [
        {
          index: 0,
          serial_number: "HB-1",
          hashrate: { value: 50, unit: MetricUnit.THS },
          efficiency: { value: 30, unit: MetricUnit.JTH },
          power: { value: 1_500, unit: MetricUnit.W },
          temperature: { average: 65, inlet: 40, outlet: 70, unit: MetricUnit.ValueC },
          asics: {
            hashrate: { values: [0.5], unit: MetricUnit.THS },
            temperature: { values: [72], unit: MetricUnit.ValueC },
          },
        },
      ],
      timestamp: "2026-08-17T00:00:00Z",
    });
    const history = toCanonicalTimeSeries(
      {
        meta: { interval: "PT1M" },
        data: {
          miner: { hashrate: { unit: MetricUnit.THS, values: [90, 100] } },
          hashboards: [{ serial_number: "HB-1", hashrate: { unit: MetricUnit.THS, values: [45, 50] } }],
        },
      },
      { startTimeMs: 1_000 },
    );

    expect(latest.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.latest).toEqual({ value: 100, units: "TH/s" });
    expect(latest.asics.get(getAsicId("HB-1", 0))?.temperature?.latest).toEqual({
      value: 72,
      units: "°C",
    });
    expect(history.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.points).toEqual([
      { timestampMs: 1_000, value: 90 },
      { timestampMs: 61_000, value: 100 },
    ]);
    expect(history.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.units).toBe("TH/s");
    expect(history.hashboards.get(asHashboardId("serial:HB-1"))?.hashrate?.timeSeries?.bucketSizeMs).toBe(60_000);
  });

  it("scales latest measurements from milli/kilo units into canonical units", () => {
    const telemetry = toCanonicalTelemetry({
      timestamp: "2026-08-17T00:00:00Z",
      psus: [
        {
          index: 0,
          serial_number: "PSU-1",
          voltage: { input: 240_000, output: 12_000, unit: "mV" },
          current: { input: 10_000, output: 200_000, unit: "mA" },
          power: { input: 2.4, output: 2.3, unit: "kW" },
        },
      ],
    } as never);

    const psu = telemetry.psus.get(getPsuId(1));
    expect(psu?.inputVoltage?.latest).toEqual({ value: 240, units: "V" });
    expect(psu?.outputVoltage?.latest).toEqual({ value: 12, units: "V" });
    expect(psu?.inputCurrent?.latest).toEqual({ value: 10, units: "A" });
    expect(psu?.outputCurrent?.latest).toEqual({ value: 200, units: "A" });
    expect(psu?.inputPower?.latest).toEqual({ value: 2_400, units: "W" });
    expect(psu?.outputPower?.latest).toEqual({ value: 2_300, units: "W" });
  });

  it("applies each historical unit-scaling factor to series values and units", () => {
    // Exercises every non-unity branch of the unit table. The field carrying the
    // value is irrelevant to scaling, so all cases ride on `miner.hashrate`.
    const cases: Array<{ unit: string; raw: number; expected: number; units: string }> = [
      { unit: "MH/s", raw: 5_000, expected: 5, units: "GH/s" },
      { unit: "GH/s", raw: 5, expected: 5, units: "GH/s" },
      { unit: "kW", raw: 3, expected: 3_000, units: "W" },
      { unit: "MW", raw: 0.003, expected: 3_000, units: "W" },
      { unit: "mV", raw: 240_000, expected: 240, units: "V" },
      { unit: "mA", raw: 10_000, expected: 10, units: "A" },
      { unit: "GHz", raw: 1.2, expected: 1_200, units: "MHz" },
    ];

    for (const { unit, raw, expected, units } of cases) {
      const history = toCanonicalTimeSeries(
        { meta: { interval: "PT1M" }, data: { miner: { hashrate: { unit, values: [raw] } } } } as never,
        { startTimeMs: 0 },
      );
      const series = history.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries;
      expect(series?.units, `units for ${unit}`).toBe(units);
      expect(series?.points[0]?.value, `value for ${unit}`).toBeCloseTo(expected);
    }
  });

  it("omits latest metrics whose wire values cannot be normalized", () => {
    const telemetry = toCanonicalTelemetry({
      timestamp: "2026-08-17T00:00:00Z",
      hashboards: [
        {
          index: 0,
          serial_number: "HB-1",
          hashrate: { value: 50, unit: MetricUnit.THS },
          efficiency: { value: 30, unit: MetricUnit.JTH },
          power: { value: 1_500, unit: MetricUnit.W },
          temperature: { average: undefined, inlet: 40, outlet: 70, unit: MetricUnit.ValueC },
        },
      ],
    } as never);

    const board = telemetry.hashboards.get(asHashboardId("serial:HB-1"));
    expect(board?.temperature).toBeUndefined();
    expect(board?.inletTemperature?.latest?.value).toBe(40);
    expect(board?.outletTemperature?.latest?.value).toBe(70);
  });

  it("maps a zero-based PSU index to its one-based physical slot", () => {
    const hardware = toCanonicalHardware(
      { "psus-info": [{ psu_sn: "PSU-2", slot: 2 }] },
      { deviceId: "device:rig", profile: "r2" },
    );
    const telemetry = toCanonicalTelemetry({
      timestamp: "2026-08-17T00:00:00Z",
      psus: [
        {
          index: 1,
          serial_number: "PSU-2",
          voltage: { input: 240, output: 12, unit: MetricUnit.V },
          current: { input: 10, output: 200, unit: MetricUnit.A },
          power: { input: 2_400, output: 2_300, unit: MetricUnit.W },
          temperature: { ambient: 35, average: 45, hotspot: 60, unit: MetricUnit.ValueC },
        },
      ],
    });

    expect(hardware.psus.has(getPsuId(2))).toBe(true);
    expect(telemetry.psus.get(getPsuId(2))?.inputVoltage?.latest?.value).toBe(240);
  });

  it("does not attribute ASIC history through ambiguous hashboard indexes", () => {
    const history = toCanonicalTimeSeries(
      {
        meta: { interval: "PT1M" },
        data: {
          hashboards: [
            { index: 0, serial_number: "HB-1" },
            { index: 0, serial_number: "HB-2" },
          ],
          asics: [{ hashboard_index: 0, index: 2, temperature: { unit: MetricUnit.ValueC, values: [70] } }],
        },
      },
      { startTimeMs: 1_000 },
    );

    expect(history.asics.size).toBe(0);
  });

  it("normalizes network, status, pool, target, and error reads without wire types", () => {
    expect(
      toCanonicalNetworkInfo({
        hostname: "proto-rig",
        ip: "192.0.2.10",
        mac: "00:11:22:33:44:55",
        gateway: "192.0.2.1",
        netmask: "255.255.255.0",
        dhcp: true,
      }).interfaces.get("primary"),
    ).toMatchObject({ ipAddress: "192.0.2.10", macAddress: "00:11:22:33:44:55", isPrimary: true });

    expect(
      toCanonicalMiningStatus({ status: "Mining", mining_uptime_s: 60, reboot_uptime_s: 120, hw_errors: 2 }),
    ).toMatchObject({
      owner: { kind: "module", id: "module:root" },
      resource: { status: "ready" },
      miningStatus: "mining",
      miningUptime: { value: 60, units: "s" },
      rebootUptime: { value: 120, units: "s" },
      hardwareErrorCount: 2,
    });

    expect(
      toCanonicalPools([
        { id: 2, priority: 2, url: "stratum+tcp://backup:3333", user: "worker.2" },
        { id: 1, priority: 0, url: "stratum+tcp://primary:3333", user: "worker.1", accepted: 10 },
      ]).map((pool) => pool.id),
    ).toEqual(["pool:1", "pool:2"]);

    expect(
      toCanonicalMiningTarget({
        power_target_watts: 3_000,
        default_power_target_watts: 2_500,
        power_target_min_watts: 400,
        power_target_max_watts: 3_500,
        performance_mode: PerformanceMode.MaximumHashrate,
      }),
    ).toEqual({
      moduleId: "module:root",
      resource: expect.objectContaining({ status: "ready" }),
      valueWatts: 3_000,
      defaultWatts: 2_500,
      boundsWatts: { min: 400, max: 3_500 },
      performanceMode: "maximumHashrate",
    });

    expect(
      toCanonicalErrors([{ error_code: "FanSlow", message: "Fan stalled", source: "fan", slot: 1 }])[0],
    ).toMatchObject({
      code: "FanSlow",
      message: "Fan stalled",
      source: { kind: "fan", slot: 1 },
      component: { id: "module:root:fan:1", kind: "fan" },
      severity: "critical",
    });
  });

  it("preserves error source and location without inventing unresolved component ids", () => {
    const [fanError, hashboardError] = toCanonicalErrors([
      { error_code: "FanUnknown", source: "fan" },
      { error_code: "BoardFault", source: "hashboard", slot: 2 },
    ]);

    expect(fanError).toMatchObject({ source: { kind: "fan" }, component: undefined });
    expect(hashboardError).toMatchObject({
      source: { kind: "hashboard", slot: 2 },
      component: undefined,
    });
  });

  it("maps cooling modes and fan telemetry at the v1 boundary", () => {
    expect(
      toCanonicalCoolingStatus({
        fan_mode: "Auto",
        fans: [{ slot: 2, rpm: 4_800, percentage: 75 }],
      }),
    ).toEqual({
      mode: "automatic",
      fans: [
        {
          componentId: "module:root:fan:2",
          rpm: { latest: { value: 4_800, units: "RPM" } },
          speedPercent: { latest: { value: 75, units: "%" } },
        },
      ],
    });
    expect(toV1CoolingMode("off")).toBe("Off");
    expect(toV1CoolingMode("manual")).toBe("Manual");
  });

  it("unwraps the v1 system-tag response", () => {
    expect(toCanonicalSystemTag({ tag: "rig-01" })).toBe("rig-01");
    expect(toCanonicalSystemTag("rig-02")).toBe("rig-02");
  });
});
