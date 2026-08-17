import { describe, expect, it } from "vitest";
import { MetricUnit, PerformanceMode } from "@/protoOS/api/generatedApi";
import { ROOT_MODULE_KEY, asHashboardId } from "@/protoOS/store/contract";
import {
  normalizeV1Errors,
  normalizeV1Hardware,
  normalizeV1MiningStatus,
  normalizeV1MiningTarget,
  normalizeV1NetworkInfo,
  normalizeV1Pools,
  normalizeV1SystemInfo,
  normalizeV1Telemetry,
  normalizeV1TimeSeries,
} from "./normalize";

describe("MDK v1 normalization", () => {
  it("maps a CU model to the container product family and CT1 module profile", () => {
    const systemInfo = normalizeV1SystemInfo({
      product_name: "Proto CU1",
      model: "CU1",
      cb_sn: "controller-1",
      os: { name: "ProtoOS", version: "1.8.2" },
    });

    expect(systemInfo.identity.deviceType).toBe("container");
    expect(systemInfo.moduleProfile).toBe("ct1");
    expect(systemInfo.identity.serialNumber).toBe("controller-1");
  });

  it("builds a deterministic synthetic R2 module and collision-safe component ids", () => {
    const hardware = normalizeV1Hardware(
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
    const latest = normalizeV1Telemetry({
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
        },
      ],
      timestamp: "2026-08-17T00:00:00Z",
    });
    const history = normalizeV1TimeSeries(
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
    expect(history.rollupsByEntity.get(ROOT_MODULE_KEY)?.hashrate?.timeSeries?.points).toEqual([
      { timestampMs: 1_000, value: 90 },
      { timestampMs: 61_000, value: 100 },
    ]);
    expect(history.hashboards.get(asHashboardId("serial:HB-1"))?.hashrate?.timeSeries?.bucketSizeMs).toBe(60_000);
  });

  it("normalizes network, status, pool, target, and error reads without wire types", () => {
    expect(
      normalizeV1NetworkInfo({
        hostname: "proto-rig",
        ip: "192.0.2.10",
        mac: "00:11:22:33:44:55",
        gateway: "192.0.2.1",
        netmask: "255.255.255.0",
        dhcp: true,
      }).interfaces.get("primary"),
    ).toMatchObject({ ipAddress: "192.0.2.10", macAddress: "00:11:22:33:44:55", isPrimary: true });

    expect(
      normalizeV1MiningStatus({ status: "Mining", mining_uptime_s: 60, reboot_uptime_s: 120, hw_errors: 2 }),
    ).toMatchObject({
      status: "Mining",
      miningUptime: { value: 60, units: "s" },
      rebootUptime: { value: 120, units: "s" },
      hardwareErrorCount: 2,
    });

    expect(
      normalizeV1Pools([
        { id: 2, priority: 2, url: "stratum+tcp://backup:3333", user: "worker.2" },
        { id: 1, priority: 0, url: "stratum+tcp://primary:3333", user: "worker.1", accepted: 10 },
      ]).map((pool) => pool.id),
    ).toEqual(["pool:1", "pool:2"]);

    expect(
      normalizeV1MiningTarget({
        power_target_watts: 3_000,
        default_power_target_watts: 2_500,
        power_target_min_watts: 400,
        power_target_max_watts: 3_500,
        performance_mode: PerformanceMode.MaximumHashrate,
      }),
    ).toEqual({
      valueWatts: 3_000,
      defaultWatts: 2_500,
      bounds: { minWatts: 400, maxWatts: 3_500 },
      performanceMode: "performance",
    });

    expect(
      normalizeV1Errors([{ error_code: "FanSlow", message: "Fan stalled", source: "fan", slot: 1 }])[0],
    ).toMatchObject({
      code: "FanSlow",
      message: "Fan stalled",
      component: { id: "module:root:fan:1", kind: "fan" },
    });
  });
});
