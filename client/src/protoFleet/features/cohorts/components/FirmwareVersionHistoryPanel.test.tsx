import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import { buildFirmwareSeries, firmwareColor } from "./firmwareVersionHistory";
import {
  CohortFirmwareVersionCountSchema,
  CohortFirmwareVersionHistoryPointSchema,
  GetCohortFirmwareVersionHistoryResponseSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";

const buildHistory = (versions: Array<{ firmwareVersion: string; deviceCount: number }>) =>
  create(GetCohortFirmwareVersionHistoryResponseSchema, {
    memberCount: versions.reduce((total, version) => total + version.deviceCount, 0),
    points: [
      create(CohortFirmwareVersionHistoryPointSchema, {
        timestamp: create(TimestampSchema, { seconds: 1n }),
        versions: versions.map((version) => create(CohortFirmwareVersionCountSchema, version)),
      }),
    ],
  });

describe("FirmwareVersionHistoryPanel", () => {
  it("keeps the five largest versions and groups the remainder separately from Unknown", () => {
    const series = buildFirmwareSeries(
      buildHistory([
        { firmwareVersion: "6.0", deviceCount: 1 },
        { firmwareVersion: "1.0", deviceCount: 6 },
        { firmwareVersion: "2.0", deviceCount: 5 },
        { firmwareVersion: "3.0", deviceCount: 4 },
        { firmwareVersion: "4.0", deviceCount: 3 },
        { firmwareVersion: "5.0", deviceCount: 2 },
        { firmwareVersion: "", deviceCount: 2 },
      ]),
    );

    expect(series.map((item) => item.label)).toEqual(["1.0", "2.0", "3.0", "4.0", "5.0", "Unknown", "Other"]);
    expect(series.find((item) => item.label === "Other")?.versions).toEqual(["6.0"]);
    expect(series.find((item) => item.label === "Unknown")?.versions).toEqual([""]);
  });

  it("assigns a stable color from the firmware version", () => {
    expect(firmwareColor("1.3.6")).toBe(firmwareColor("1.3.6"));
    expect(firmwareColor("1.3.6")).not.toBe("");
  });
});
