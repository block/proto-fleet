import { expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  parseReportTime,
  reportDate,
  reportPeriod,
  reportPeriodSummary,
  reportTime,
  reportTimeInput,
} from "./reportFormat";
import { ReportingPeriodSchema } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

it.each([
  ["Africa/Nairobi", "2026-10-01T00:00", "2026-09-30T21:00:00.000Z"],
  ["America/New_York", "2026-07-01T00:00", "2026-07-01T04:00:00.000Z"],
  ["America/New_York", "2026-01-01T00:00", "2026-01-01T05:00:00.000Z"],
])("converts manual report times in %s", (zone, value, utc) => {
  const time = parseReportTime(value, zone)!;
  expect(new Date(Number(time.seconds) * 1000).toISOString()).toBe(utc);
  expect(reportTimeInput(time, zone)).toBe(value);
});
it("rejects nonexistent and ambiguous daylight-saving times", () => {
  expect(parseReportTime("2026-03-08T02:30", "America/New_York")).toBeUndefined();
  expect(parseReportTime("2026-11-01T01:30", "America/New_York")).toBeUndefined();
});
it("renders unsupported report zones using an explicit UTC fallback", () => {
  for (const format of [reportDate, reportTime])
    expect(
      format({ $typeName: "google.protobuf.Timestamp", seconds: 1791000000n, nanos: 0 }, "Unsupported/Zone"),
    ).toContain("UTC (report timezone unavailable)");
});

it.each([
  ["monthly", "Africa/Nairobi", "2026-09-30T21:00:00Z", "2026-10-31T21:00:00Z", /^Monthly · Oct 2026$/],
  ["quarterly", "UTC", "2026-02-01T00:00:00Z", "2026-05-01T00:00:00Z", /^Quarterly · Feb.*Apr 2026$/],
  ["annual", "UTC", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z", /^Annual · 2026$/],
  ["annual", "UTC", "2025-10-01T00:00:00Z", "2026-10-01T00:00:00Z", /^Annual · Oct 2025.*Sep 2026$/],
  ["ad_hoc", "UTC", "2026-11-02T05:28:00Z", "2026-11-02T05:29:00Z", /^Ad hoc · Nov 2, 2026$/],
])("summarizes %s dates in the reporting calendar", (kind, timezone, start, end, expected) => {
  const period = create(ReportingPeriodSchema, {
    kind,
    timezone,
    start: timestampFromDate(new Date(start)),
    end: timestampFromDate(new Date(end)),
  });
  expect(reportPeriodSummary(period)).toMatch(expected);
});

it("keeps unavailable periods and unsupported calendars explicit in summaries", () => {
  expect(reportPeriodSummary()).toBe("—");
  const period = create(ReportingPeriodSchema, {
    kind: "monthly",
    timezone: "Unsupported/Zone",
    start: timestampFromDate(new Date("2026-10-01T00:00:00Z")),
    end: timestampFromDate(new Date("2026-11-01T00:00:00Z")),
  });
  expect(reportPeriodSummary(period)).toContain("UTC (report timezone unavailable)");
});

it("shows a compact exact window with the reporting timezone once", () => {
  const period = create(ReportingPeriodSchema, {
    kind: "ad_hoc",
    timezone: "UTC",
    start: timestampFromDate(new Date("2026-11-02T05:28:00Z")),
    end: timestampFromDate(new Date("2026-11-02T05:29:00Z")),
  });
  const text = reportPeriod(period);
  expect(text).toContain("5:28");
  expect(text).toContain("5:29");
  expect(text.match(/Nov 2, 2026/g)).toHaveLength(1);
  expect(text.match(/UTC/g)).toHaveLength(1);
  expect(text).not.toContain("Ad hoc");
  expect(reportPeriod({ ...period, timezone: "Unsupported/Zone" })).toContain("UTC (report timezone unavailable)");
  expect(reportPeriod({ ...period, start: period.end, end: period.start })).toBe("Reporting period unavailable");
  expect(reportPeriod()).toBe("Reporting period unavailable");
});
