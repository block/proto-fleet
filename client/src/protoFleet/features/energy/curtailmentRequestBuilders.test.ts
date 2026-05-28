import { describe, expect, it } from "vitest";

import {
  buildStartCurtailmentRequest,
  buildUpdateCurtailmentEventRequest,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

const baseValues: CurtailmentSubmitValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: "customPlan",
  curtailmentMode: "fixedKwReduction",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "40",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  reason: "Grid peak",
  includeMaintenance: false,
};

describe("curtailmentRequestBuilders", () => {
  it("builds optional uint32-backed settings from valid whole-number inputs", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      minDurationSec: "300",
      maxDurationSec: "1800",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
    });

    expect(request.minCurtailedDurationSec).toBe(300);
    expect(request.maxDurationSeconds).toBe(1800);
    expect(request.restoreBatchSize).toBe(10);
    expect(request.restoreBatchIntervalSec).toBe(120);
  });

  it("keeps unsupported scope state from falling back to the whole fleet", () => {
    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        scopeType: "deviceSet",
        scopeId: "racks",
        deviceSetIds: ["rack-1"],
      }),
    ).toThrow("Unsupported curtailment target scope.");

    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceIdentifiers: [],
      }),
    ).toThrow("Unsupported curtailment target scope.");
  });

  it("rejects invalid uint32-backed settings", () => {
    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        restoreBatchSize: "-1",
      }),
    ).toThrow("Enter restore batch size of 0 or more.");

    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        restoreIntervalSec: "1.5",
      }),
    ).toThrow("Enter restore batch interval as a whole number.");

    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        maxDurationSec: "604801",
      }),
    ).toThrow("Enter max duration of 604,800 or less.");
  });

  it("builds update requests with changed operator-safe fields only", () => {
    const request = buildUpdateCurtailmentEventRequest(
      "curt-1",
      {
        ...baseValues,
        reason: "  Updated grid peak  ",
        maxDurationSec: "1800",
        restoreBatchSize: "",
        restoreIntervalSec: "120",
      },
      {
        ...baseValues,
        reason: "Grid peak",
        maxDurationSec: "1800",
        restoreBatchSize: "",
        restoreIntervalSec: "60",
      },
    );

    expect(request).toEqual(
      expect.objectContaining({
        eventUuid: "curt-1",
        reason: "Updated grid peak",
        restoreBatchIntervalSec: 120,
      }),
    );
    expect(request.maxDurationSeconds).toBeUndefined();
    expect(request.restoreBatchSize).toBeUndefined();
  });

  it("sends zero when an update clears a numeric setting", () => {
    const request = buildUpdateCurtailmentEventRequest(
      "curt-1",
      {
        ...baseValues,
        restoreBatchSize: "",
      },
      {
        ...baseValues,
        restoreBatchSize: "10",
      },
    );

    expect(request.restoreBatchSize).toBe(0);
  });
});
