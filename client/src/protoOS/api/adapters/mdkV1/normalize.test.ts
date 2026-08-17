import { describe, expect, it } from "vitest";
import { MetricUnit } from "@/protoOS/api/generatedApi";
import { ROOT_MODULE_KEY, asHashboardId } from "@/protoOS/store/contract";
import { normalizeV1Hardware, normalizeV1SystemInfo, normalizeV1Telemetry, normalizeV1TimeSeries } from "./normalize";

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
});
