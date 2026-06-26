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
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
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
      curtailBatchSize: "25",
      curtailBatchIntervalSec: "60",
      restoreBatchSize: "10",
      restoreIntervalSec: "120",
    });

    expect(request.minCurtailedDurationSec).toBe(300);
    expect(request.maxDurationSeconds).toBe(1800);
    expect(request.curtailBatchSize).toBe(25);
    expect(request.curtailBatchIntervalSec).toBe(60);
    expect(request.restoreBatchSize).toBe(10);
    expect(request.restoreBatchIntervalSec).toBe(120);
  });

  it("omits curtail batch settings when the start form leaves them blank", () => {
    const request = buildStartCurtailmentRequest(baseValues);

    expect(request.curtailBatchSize).toBeUndefined();
    expect(request.curtailBatchIntervalSec).toBeUndefined();
  });

  it("requires curtail batch size when the interval field is present", () => {
    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        curtailBatchIntervalSec: "0",
      }),
    ).toThrow("Enter curtail batch size before adding a curtail batch interval.");
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

    expect(request.scopes).toHaveLength(1);
    expect(request.scopes[0]?.scope.case).toBe("site");
    if (request.scopes[0]?.scope.case !== "site") {
      throw new Error("Expected site scope");
    }
    expect(request.scopes[0].scope.value.siteId).toBe(42n);
  });

  it("builds combined site and miner scopes without expanding sites", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      scopeType: "explicitMiners",
      siteSelection: "site",
      scopeId: "site-42",
      siteId: "42",
      deviceIdentifiers: ["miner-1", "miner-1", "miner-2"],
    });

    expect(request.scopes).toHaveLength(2);
    expect(request.scopes[0]?.scope.case).toBe("site");
    expect(request.scopes[1]?.scope.case).toBe("deviceIdentifiers");
    if (request.scopes[0]?.scope.case !== "site" || request.scopes[1]?.scope.case !== "deviceIdentifiers") {
      throw new Error("Expected site and deviceIdentifiers scopes");
    }
    expect(request.scopes[0].scope.value.siteId).toBe(42n);
    expect(request.scopes[1].scope.value.deviceIdentifiers).toEqual(["miner-1", "miner-2"]);
  });

  it("builds multiple site scopes with explicit miner scopes without expanding sites", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      scopeType: "explicitMiners",
      siteSelection: "site",
      scopeId: "2 sites",
      siteId: "42",
      siteIds: ["42", "43", "42"],
      deviceIdentifiers: ["miner-1", "miner-2"],
    });

    expect(request.scopes).toHaveLength(3);
    expect(request.scopes.map((scope) => scope.scope.case)).toEqual(["site", "site", "deviceIdentifiers"]);
    if (
      request.scopes[0]?.scope.case !== "site" ||
      request.scopes[1]?.scope.case !== "site" ||
      request.scopes[2]?.scope.case !== "deviceIdentifiers"
    ) {
      throw new Error("Expected two site scopes and one deviceIdentifiers scope");
    }
    expect(request.scopes[0].scope.value.siteId).toBe(42n);
    expect(request.scopes[1].scope.value.siteId).toBe(43n);
    expect(request.scopes[2].scope.value.deviceIdentifiers).toEqual(["miner-1", "miner-2"]);
  });

  it("builds all-sites scopes from the selected site ids", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      scopeType: "explicitMiners",
      siteSelection: "allSites",
      siteId: "42",
      siteIds: ["42", "43"],
      deviceIdentifiers: ["miner-1", "miner-2"],
    });

    expect(request.scopes).toHaveLength(3);
    expect(request.scopes.map((scope) => scope.scope.case)).toEqual(["site", "site", "deviceIdentifiers"]);
    if (
      request.scopes[0]?.scope.case !== "site" ||
      request.scopes[1]?.scope.case !== "site" ||
      request.scopes[2]?.scope.case !== "deviceIdentifiers"
    ) {
      throw new Error("Expected all-sites scope to preserve selected sites");
    }
    expect(request.scopes[0].scope.value.siteId).toBe(42n);
    expect(request.scopes[1].scope.value.siteId).toBe(43n);
    expect(request.scopes[2].scope.value.deviceIdentifiers).toEqual(["miner-1", "miner-2"]);
  });

  it("collapses all-miner selection to whole org without sending page-loaded miner ids", () => {
    const request = buildStartCurtailmentRequest({
      ...baseValues,
      scopeType: "wholeOrg",
      siteSelection: "site",
      siteId: "42",
      minerSelectionMode: "all",
      deviceIdentifiers: ["miner-1", "miner-2"],
    });

    expect(request.scopes).toHaveLength(1);
    expect(request.scopes[0]?.scope.case).toBe("wholeOrg");
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
        curtailBatchSize: "0",
      }),
    ).toThrow("Enter curtail batch size greater than 0.");

    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        curtailBatchIntervalSec: "30",
      }),
    ).toThrow("Enter curtail batch size before adding a curtail batch interval.");

    expect(() =>
      buildStartCurtailmentRequest({
        ...baseValues,
        curtailBatchSize: "5",
        curtailBatchIntervalSec: "3601",
      }),
    ).toThrow("Enter curtail batch interval of 3,600 or less.");

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
