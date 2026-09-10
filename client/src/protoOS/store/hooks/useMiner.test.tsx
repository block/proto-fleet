import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { toCanonicalTelemetry } from "../../api/adapters/mdkV1/normalize";
import { asComponentId, ROOT_MODULE_ID, ROOT_MODULE_KEY } from "../contract";
import useMinerStore from "../useMinerStore";
import { getHashboardId } from "../utils/entityIds";
import { getAsicId } from "../utils/getAsicId";
import { useHashboardSerialsByBay, useHashboardsHardware, useMinerHardware } from "./useHardware";
import { useChartDataForMetric, useMiner, useMinerHashboardAsics } from "./useMiner";
import { useErrors } from "./useMinerStatus";
import { useFansTelemetry, useMinerTelemetry } from "./useTelemetry";
import { MetricUnit } from "@/protoOS/api/adapters/mdkV1";

describe("useMiner", () => {
  beforeEach(() => {
    useMinerStore.getState().resetDeviceData();
  });

  it("returns a stable snapshot after normalized hardware and telemetry hydrate", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map([[ROOT_MODULE_KEY, { hashrate: { latest: { value: 100, units: "TH/s" } } }]]),
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useMiner());

    expect(result.current).toMatchObject({ hashboardSerials: ["HB-1"] });
    expect(result.current?.hashrate?.latest).toEqual({ value: 100, units: "TH/s" });
  });

  it("keeps compatibility hardware and telemetry selector snapshots stable", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.updateFanTelemetry(1, {
      rpm: { latest: { value: 4_800, units: "RPM" } },
    });

    const { result } = renderHook(() => ({
      miner: useMinerHardware(),
      hashboards: useHashboardsHardware(),
      telemetry: useMinerTelemetry(),
      fans: useFansTelemetry(),
    }));

    expect(result.current.miner?.hashboardSerials).toEqual(["HB-1"]);
    expect(result.current.hashboards).toHaveLength(1);
    expect(result.current.fans[0]?.rpm?.latest?.value).toBe(4_800);
  });

  it("keeps compatibility error selector snapshots stable", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-1",
        code: "FanSpeedTooLow",
        message: "Fan 1 is below its minimum speed",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        component: {
          kind: "fan",
          id: asComponentId("fan:1"),
          owner: { kind: "module", id: ROOT_MODULE_ID },
        },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([
      {
        errorCode: "FanSpeedTooLow",
        message: "Fan 1 is below its minimum speed",
        slot: 1,
        source: "FAN",
      },
    ]);
  });

  it("groups slot-less errors by their preserved source", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-slotless-fan",
        code: "FanUnknown",
        message: "Fan subsystem unavailable",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        source: { kind: "fan" },
        severity: "critical",
      },
      {
        id: "error-hashboard-2",
        code: "BoardFault",
        message: "Hashboard fault",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        source: { kind: "hashboard", slot: 2 },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([
      expect.objectContaining({ source: "FAN", slot: undefined }),
      expect.objectContaining({ source: "HASHBOARD", slot: 2 }),
    ]);
  });

  it("maps a PSU-sourced error to the PSU source with its slot", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-psu-3",
        code: "PsuFault",
        message: "PSU 3 fault",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        source: { kind: "psu", slot: 3 },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([expect.objectContaining({ source: "PSU", slot: 3 })]);
  });

  it("falls back to the rig source with no slot when an error has neither source nor component", () => {
    useMinerStore.getState().minerStatus.setErrors([
      {
        id: "error-generic",
        code: "SystemFault",
        message: "Something went wrong",
        owner: { kind: "module", id: ROOT_MODULE_ID },
        severity: "critical",
      },
    ]);

    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([expect.objectContaining({ source: "RIG", slot: undefined })]);
  });

  it("returns no errors when the owner map has no entry", () => {
    // resetDeviceData() in beforeEach leaves errorsByOwner empty for the root module.
    const { result } = renderHook(() => useErrors());

    expect(result.current).toEqual([]);
  });

  it("joins normalized ASIC temperatures to hashboard-status positions", () => {
    const asicId = getAsicId("HB-1", 0);
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1, asicIds: [asicId] }]);
    useMinerStore
      .getState()
      .hardware.batchAddAsics([{ id: asicId, hashboardSerial: "HB-1", index: 0, row: 0, column: 0 }]);
    useMinerStore.getState().telemetry.mergeTelemetry(
      toCanonicalTelemetry({
        timestamp: "2026-08-18T00:00:00Z",
        hashboards: [
          {
            index: 0,
            serial_number: "HB-1",
            efficiency: { value: 30, unit: MetricUnit.JTH },
            hashrate: { value: 0.5, unit: MetricUnit.THS },
            power: { value: 15, unit: MetricUnit.W },
            temperature: { average: 65, inlet: 40, outlet: 70, unit: MetricUnit.ValueC },
            asics: {
              hashrate: { values: [0.5], unit: MetricUnit.THS },
              temperature: { values: [72], unit: MetricUnit.ValueC },
            },
          },
        ],
      }),
    );

    const { result } = renderHook(() => useMinerHashboardAsics("HB-1"));

    expect(result.current).toEqual([
      expect.objectContaining({
        id: "serial:HB-1:asic:0",
        index: 0,
        row: 0,
        column: 0,
        temperature: { latest: { value: 72, units: "°C" } },
      }),
    ]);
  });

  it("does not fabricate chart timestamps when bucket metadata is absent", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map([
        [
          ROOT_MODULE_KEY,
          {
            hashrate: {
              timeSeries: {
                points: [
                  { timestampMs: 1_000, value: 90 },
                  { timestampMs: 2_000, value: 100 },
                ],
              },
            },
          },
        ],
      ]),
      hashboards: new Map(),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.chartData).toEqual([]);
  });

  it("builds a multi-series chart from bucketed points, filling gaps and null values with null", () => {
    useMinerStore.getState().hardware.setHashboards([{ serial: "HB-1", slot: 1 }]);
    useMinerStore.getState().telemetry.mergeTelemetry({
      resourceByScope: new Map(),
      rollupsByEntity: new Map([
        [
          ROOT_MODULE_KEY,
          {
            hashrate: {
              timeSeries: {
                bucketSizeMs: 1_000,
                points: [
                  { timestampMs: 10_000, value: 90 },
                  { timestampMs: 11_000, value: 100 },
                  { timestampMs: 12_000, value: null },
                ],
              },
            },
          },
        ],
      ]),
      hashboards: new Map([
        [
          getHashboardId("HB-1"),
          {
            hashboardId: getHashboardId("HB-1"),
            hashrate: {
              timeSeries: {
                bucketSizeMs: 1_000,
                // Shorter than the miner series and carrying a null: exercises the
                // `?? null` fallback (index 1) and the missing-index gap (index 2).
                points: [
                  { timestampMs: 10_000, value: 45 },
                  { timestampMs: 11_000, value: null },
                ],
              },
            },
          },
        ],
      ]),
      asics: new Map(),
      psus: new Map(),
      fans: new Map(),
      plant: new Map(),
      coolingModeByOwner: new Map(),
      asicLoadByHashboard: new Map(),
    });

    const { result } = renderHook(() => useChartDataForMetric("hashrate"));

    expect(result.current.chartLines).toEqual(["miner", "HB-1"]);
    expect(result.current.chartData).toEqual([
      { datetime: 10_000, miner: 90, "HB-1": 45 },
      { datetime: 11_000, miner: 100, "HB-1": null },
      { datetime: 12_000, miner: null },
    ]);
    expect(result.current.xAxisDomain?.[0]).toBe(10_000);
  });

  it("derives fallback bays from one-based hashboard slots", () => {
    useMinerStore.getState().hardware.setHashboards([
      { serial: "HB-3", slot: 3 },
      { serial: "HB-4", slot: 4, bay: 2 },
    ]);

    const { result } = renderHook(() => useHashboardSerialsByBay());

    expect(result.current).toEqual({
      1: [null, null, "HB-3"],
      2: ["HB-4", null, null],
    });
  });
});
