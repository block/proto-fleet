import { describe, expect, it } from "vitest";

import { CurtailmentMode } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  buildStartCurtailmentRequest,
  buildUpdateCurtailmentEventRequest,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

const baseValues: CurtailmentSubmitValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  siteId: "",
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
  it("builds fixed-kW start requests with fixed-kW mode params", () => {
    const request = buildStartCurtailmentRequest(baseValues);

    expect(request.mode).toBe(CurtailmentMode.FIXED_KW);
    expect(request.modeParams.case).toBe("fixedKw");
    if (request.modeParams.case !== "fixedKw") {
      throw new Error("Expected fixedKw mode params");
    }
    expect(request.modeParams.value.targetKw).toBe(40);
  });

  it("builds full-fleet start requests without fixed-kW mode params", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      toleranceKw: "",
    });

    expect(request.mode).toBe(CurtailmentMode.FULL_FLEET);
    expect(request.modeParams.case).toBeUndefined();
  });

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

  it("builds site-scoped start requests", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      scopeType: "site",
      scopeId: "site-42",
      siteId: " 42 ",
    });

    expect(request.scope.case).toBe("site");
    if (request.scope.case !== "site") {
      throw new Error("Expected site scope");
    }
    expect(request.scope.value.siteId).toBe(42n);
  });

  it("rejects invalid site ids through the controlled scope error", () => {
    for (const siteId of ["site-42", "0", "9223372036854775808"]) {
      expect(() =>
        buildStartCurtailmentRequest({
          ...baseValues,
          scopeType: "site",
          scopeId: "site-bad",
          siteId,
        }),
      ).toThrow("Unsupported curtailment target scope.");
    }
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

  it("does not include restore batch size in update requests", () => {
    const request = buildUpdateCurtailmentEventRequest(
      "curt-1",
      {
        ...baseValues,
        reason: "Updated grid peak",
        restoreBatchSize: "20",
      },
      {
        ...baseValues,
        reason: "Grid peak",
        restoreBatchSize: "10",
      },
    );

    expect(request.reason).toBe("Updated grid peak");
    expect(request.restoreBatchSize).toBeUndefined();
  });

  it("does not send zero when an update clears max duration", () => {
    const request = buildUpdateCurtailmentEventRequest(
      "curt-1",
      {
        ...baseValues,
        reason: "Updated grid peak",
        maxDurationSec: "",
      },
      {
        ...baseValues,
        reason: "Grid peak",
        maxDurationSec: "1800",
      },
    );

    expect(request.reason).toBe("Updated grid peak");
    expect(request.maxDurationSeconds).toBeUndefined();
  });

  it("does not send zero when an update clears restore interval", () => {
    const request = buildUpdateCurtailmentEventRequest(
      "curt-1",
      {
        ...baseValues,
        reason: "Updated grid peak",
        restoreIntervalSec: "",
      },
      {
        ...baseValues,
        reason: "Grid peak",
        restoreIntervalSec: "60",
      },
    );

    expect(request.reason).toBe("Updated grid peak");
    expect(request.restoreBatchIntervalSec).toBeUndefined();
  });

  it("rejects zero max duration updates", () => {
    expect(() =>
      buildUpdateCurtailmentEventRequest(
        "curt-1",
        {
          ...baseValues,
          maxDurationSec: "0",
        },
        {
          ...baseValues,
          maxDurationSec: "1800",
        },
      ),
    ).toThrow("Enter max duration greater than 0.");
  });

  it("rejects zero restore interval updates", () => {
    expect(() =>
      buildUpdateCurtailmentEventRequest(
        "curt-1",
        {
          ...baseValues,
          restoreIntervalSec: "0",
        },
        {
          ...baseValues,
          restoreIntervalSec: "60",
        },
      ),
    ).toThrow("Enter restore batch interval greater than 0.");
  });
});
