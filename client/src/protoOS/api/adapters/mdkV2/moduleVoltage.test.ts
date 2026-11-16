import { describe, expect, test, vi } from "vitest";
import { Code } from "@connectrpc/connect";
import { MODULE_VOLTAGE_OUTCOME_UNKNOWN } from "../moduleVoltage";
import { V2ModuleVoltageReadiness } from "./generated/generatedApi";
import { MdkV2Adapter, type MdkV2Client } from "./MdkV2Adapter";
import { ct1ReportedModuleId } from "./normalize";

const actor = { headers: { "X-Frigate-Actor": "proto-os" } };
const adapterWith = (overrides: Partial<MdkV2Client>) => new MdkV2Adapter(overrides as unknown as MdkV2Client);

describe("module voltage", () => {
  test("reads fixed and automatic settings without inventing missing ranges", async () => {
    const list = vi.fn().mockResolvedValue({
      data: {
        settings: [
          {
            moduleId: "0011223344556677",
            fixedMillivolts: 28_000,
            supportedMinimumMillivolts: 26_000,
            supportedMaximumMillivolts: 30_000,
            boardCount: 3,
            readiness: V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_READY,
            updatedBy: "proto-os",
            updatedAt: "2026-11-16T10:00:00Z",
          },
          // Connect emits zero-valued scalars; these zeros mean "no range", not 0 mV.
          {
            moduleId: "8899aabbccddeeff",
            supportedMinimumMillivolts: 0,
            supportedMaximumMillivolts: 0,
            boardCount: 2,
            readiness: V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_LIMITS_UNAVAILABLE,
            readinessMessage: "firmware voltage limits are unavailable for one or more hashboards",
          },
          // Frigate reports the inverted bounds of disjoint member ranges.
          {
            moduleId: "1111222233334444",
            supportedMinimumMillivolts: 29_000,
            supportedMaximumMillivolts: 27_000,
            boardCount: 2,
            readiness: V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_NO_COMMON_RANGE,
          },
          { moduleId: "" },
        ],
      },
    });
    const settings = await adapterWith({ miningServiceListModuleVoltageSettings: list }).listModuleVoltageSettings();

    expect(list).toHaveBeenCalledWith(actor);
    expect(settings).toEqual([
      {
        moduleId: "0011223344556677",
        fixedMillivolts: 28_000,
        supportedRange: { minimumMillivolts: 26_000, maximumMillivolts: 30_000 },
        boardCount: 3,
        readiness: "ready",
        updatedBy: "proto-os",
        updatedAt: new Date("2026-11-16T10:00:00Z"),
      },
      {
        moduleId: "8899aabbccddeeff",
        boardCount: 2,
        readiness: "limitsUnavailable",
        readinessMessage: "firmware voltage limits are unavailable for one or more hashboards",
      },
      { moduleId: "1111222233334444", boardCount: 2, readiness: "noCommonRange" },
    ]);
  });

  test("keeps a saved target outside the current range visible with that range", async () => {
    const list = vi.fn().mockResolvedValue({
      data: {
        settings: [
          {
            moduleId: "0011223344556677",
            fixedMillivolts: 31_000,
            supportedMinimumMillivolts: 26_000,
            supportedMaximumMillivolts: 30_000,
            readiness: V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_TARGET_OUTSIDE_LIMITS,
          },
        ],
      },
    });
    const [setting] = await adapterWith({ miningServiceListModuleVoltageSettings: list }).listModuleVoltageSettings();
    expect(setting).toMatchObject({
      fixedMillivolts: 31_000,
      supportedRange: { minimumMillivolts: 26_000, maximumMillivolts: 30_000 },
      readiness: "targetOutsideLimits",
    });
  });

  test("sets and clears by the controller-reported module ID and returns per-board results", async () => {
    const response = {
      data: {
        setting: { moduleId: "0011223344556677", fixedMillivolts: 28_000, boardCount: 3 },
        boardsUpdated: 2,
        results: [
          { macAddress: "aa:bb:cc:00:00:01", applied: true },
          { macAddress: "aa:bb:cc:00:00:02", applied: true, message: "recorded for the board's owner" },
          // REST omits a false proto3 bool.
          { macAddress: "aa:bb:cc:00:00:03", message: "board is reserved" },
        ],
      },
    };
    const set = vi.fn().mockResolvedValue(response);
    const clear = vi.fn().mockResolvedValue({ data: { boardsUpdated: 0, results: [] } });
    const adapter = adapterWith({
      miningServiceSetModuleFixedVoltage: set,
      miningServiceClearModuleFixedVoltage: clear,
    });

    await expect(adapter.setModuleFixedVoltage("container:ct1:module:0011223344556677", 28_000)).resolves.toEqual({
      setting: { moduleId: "0011223344556677", fixedMillivolts: 28_000, boardCount: 3, readiness: "unknown" },
      boardsUpdated: 2,
      boards: [
        { macAddress: "aa:bb:cc:00:00:01", applied: true },
        { macAddress: "aa:bb:cc:00:00:02", applied: true, message: "recorded for the board's owner" },
        { macAddress: "aa:bb:cc:00:00:03", applied: false, message: "board is reserved" },
      ],
    });
    expect(set).toHaveBeenCalledWith({ moduleId: "0011223344556677" }, { millivolts: 28_000 }, actor);

    await expect(adapter.clearModuleFixedVoltage("container:ct1:module:0011223344556677")).resolves.toEqual({
      boardsUpdated: 0,
      boards: [],
    });
    expect(clear).toHaveBeenCalledWith({ moduleId: "0011223344556677" }, actor);
  });

  test.each([
    ["a slot-placed module", "container:ct1:tank:2:module:5", 28_000],
    ["a fractional value", "container:ct1:module:0011223344556677", 28_000.5],
    ["a non-finite value", "container:ct1:module:0011223344556677", Number.POSITIVE_INFINITY],
    ["zero", "container:ct1:module:0011223344556677", 0],
  ])("refuses %s without sending anything", async (_name, moduleId, millivolts) => {
    const set = vi.fn();
    const adapter = adapterWith({ miningServiceSetModuleFixedVoltage: set });
    await expect(adapter.setModuleFixedVoltage(moduleId, millivolts)).rejects.toMatchObject({
      writeOutcome: "refused",
    });
    expect(set).not.toHaveBeenCalled();
  });

  const refusedCodes = new Set<number>([
    Code.InvalidArgument,
    Code.FailedPrecondition,
    Code.NotFound,
    Code.Unimplemented,
    Code.PermissionDenied,
  ]);
  test.each(
    Object.values(Code)
      .filter((code): code is Code => typeof code === "number" && code !== Code.Unauthenticated)
      .map((code) => [Code[code], code] as const),
  )("classifies write failure code %s as refusal or unknown outcome", async (_name, code) => {
    const detail = "module voltage outside limits";
    const adapter = adapterWith({
      miningServiceClearModuleFixedVoltage: vi
        .fn()
        .mockRejectedValue({ status: 400, error: { code, message: detail } }),
    });
    const failure = adapter.clearModuleFixedVoltage("container:ct1:module:0011223344556677");
    if (refusedCodes.has(code)) {
      await expect(failure).rejects.toMatchObject({
        message: `The controller refused the request: ${detail}. The voltage setting was not changed.`,
        writeOutcome: "refused",
      });
    } else {
      await expect(failure).rejects.toMatchObject({ message: MODULE_VOLTAGE_OUTCOME_UNKNOWN, writeOutcome: "unknown" });
    }
  });

  test("treats a network failure as an unknown outcome and leaves authentication failures for refresh", async () => {
    const set = vi.fn().mockRejectedValueOnce(new TypeError("Failed to fetch"));
    const adapter = adapterWith({ miningServiceSetModuleFixedVoltage: set });
    await expect(adapter.setModuleFixedVoltage("container:ct1:module:00aa", 28_000)).rejects.toMatchObject({
      writeOutcome: "unknown",
    });
    const unauthenticated = { status: 401, error: { code: Code.Unauthenticated, message: "token expired" } };
    set.mockRejectedValueOnce(unauthenticated);
    await expect(adapter.setModuleFixedVoltage("container:ct1:module:00aa", 28_000)).rejects.toBe(unauthenticated);
  });

  test("names modules only by their reported identity", () => {
    expect(ct1ReportedModuleId("container:ct1:module:0011223344556677")).toBe("0011223344556677");
    expect(ct1ReportedModuleId("container:ct1:module:ns:T1-M02")).toBe("ns:T1-M02");
    expect(ct1ReportedModuleId("container:ct1:tank:1:module:3")).toBeUndefined();
    expect(ct1ReportedModuleId("container:ct1")).toBeUndefined();
  });
});
