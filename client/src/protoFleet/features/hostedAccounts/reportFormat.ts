import { type Timestamp, timestampFromDate } from "@bufbuild/protobuf/wkt";

import type { ReportingPeriod, ReportingTarget } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { buildDateInTimeZone, getTimeZoneDateTimeParts } from "@/protoFleet/features/settings/utils/scheduleDateUtils";

export const analysisTypes: Record<string, string> = {
  miner_performance: "Miner performance",
  energy_usage: "Energy usage",
  issues_repairs: "Issues and repairs",
};

export const reportLabel = (text: string) => {
  if (text === "moduleCoverage") return "Module repair coverage";
  const words = text.replace(/_/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2");
  return words ? words[0].toUpperCase() + words.slice(1) : "Unavailable";
};

export const reportPrerequisites: Record<string, { label: string; missing: string }> = {
  G1: {
    label: "Contract definitions and formulas",
    missing: "Contract definitions and calculation methods have not been approved.",
  },
  G2: { label: "Certified sources and estate", missing: "Data sources and equipment coverage have not been verified." },
  G3: {
    label: "Signed orders and commercial coverage",
    missing: "Commercial coverage and allocation evidence have not been approved.",
  },
  G4: {
    label: "Evidence retention and capacity",
    missing: "Evidence retention, storage capacity and recovery have not been verified.",
  },
  G5: {
    label: "Calendar, representatives and delivery",
    missing: "The reporting calendar and delivery arrangements have not been approved.",
  },
};
const reportCauses: Record<string, string> = {
  module_coverage_incomplete:
    "Some boards lack verified whole-module coverage. Module counts cover only the verified portion of the report.",
  approved_contract_formula_unimplemented: "The contract calculation is not available yet.",
  estimated_meter_energy: "Energy includes estimated meter readings.",
  uncertified_meter_source: "The meter source has not been verified.",
  uncertified_hashrate_source: "The hashrate source has not been verified.",
  required_evidence_unavailable: "The evidence needed to calculate this result is missing.",
  annual_report_required: "Year-over-year comparisons are available only in annual reports.",
  ad_hoc_internal_target_unconfigured: "No internal target has been set.",
  incomplete_inputs: "Some report inputs are missing or unverified.",
  independent_exclusion_review_unavailable: "Interruption exclusions have not been independently reviewed.",
  pool_observation_freshness_rule_unimplemented: "Pool observation freshness checks are not available yet.",
  pool_observation_missing: "Pool observations are missing.",
  qualified_host_and_pool_reporting_state_unavailable: "Verified host and pool status is unavailable.",
};
export const reportCause = (cause: string) =>
  reportPrerequisites[cause.replace(/^missing_/, "")]?.missing ?? reportCauses[cause] ?? reportLabel(cause);
export const reportMetricLabel = (key: string) =>
  (({ hashrate_performance_hs: "Hashrate", metered_energy_kwh: "Metered energy" }) as Record<string, string>)[key] ??
  reportLabel(key);
const formatReportTime = (time: Timestamp | undefined, timezone: string, options: Intl.DateTimeFormatOptions) => {
  if (!time) return "Unavailable";
  const date = new Date(Number(time.seconds) * 1000);
  if (!Number.isFinite(date.getTime())) return "Unavailable";
  const zone = timezone || "UTC";
  try {
    return `${new Intl.DateTimeFormat(undefined, { ...options, timeZone: zone }).format(date)} · ${zone}`;
  } catch {
    return `${new Intl.DateTimeFormat(undefined, { ...options, timeZone: "UTC" }).format(date)} · UTC (report timezone unavailable)`;
  }
};
export const reportTime = (time?: Timestamp, timezone = "UTC") =>
  formatReportTime(time, timezone, { dateStyle: "medium", timeStyle: "short" });
export const reportPeriod = (period?: ReportingPeriod) => {
  if (!period?.start || !period.end) return "Reporting period unavailable";
  const start = new Date(Number(period.start.seconds) * 1000);
  const end = new Date(Number(period.end.seconds) * 1000);
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || end < start)
    return "Reporting period unavailable";
  const format = (timezone: string) => {
    const day = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeZone: timezone });
    if (day.format(start) === day.format(end)) {
      const time = new Intl.DateTimeFormat(undefined, { timeStyle: "short", timeZone: timezone });
      return `${day.format(start)}, ${time.format(start)}–${time.format(end)}`;
    }
    const dateTime = new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
      timeZone: timezone,
    });
    return `${dateTime.format(start)} – ${dateTime.format(end)}`;
  };
  const zone = period.timezone || "UTC";
  try {
    return `${format(zone)} · ${zone}`;
  } catch {
    return `${format("UTC")} · UTC (report timezone unavailable)`;
  }
};

export const reportPeriodSummary = (period?: ReportingPeriod) => {
  if (!period?.start || !period.end) return "—";
  const start = new Date(Number(period.start.seconds) * 1000);
  const end = new Date(Number(period.end.seconds - (period.end.nanos === 0 ? 1n : 0n)) * 1000);
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || end < start) return "—";
  const format = (timezone: string) => {
    const first = getTimeZoneDateTimeParts(start, timezone);
    const last = getTimeZoneDateTimeParts(end, timezone);
    const calendarYear =
      first?.month === 1 && first.day === 1 && last?.month === 12 && last.day === 31 && first.year === last.year;
    const options: Intl.DateTimeFormatOptions =
      period.kind === "ad_hoc"
        ? { month: "short", day: "numeric", year: "numeric" }
        : period.kind === "annual" && calendarYear
          ? { year: "numeric" }
          : { month: "short", year: "numeric" };
    const formatter = new Intl.DateTimeFormat(undefined, { ...options, timeZone: timezone });
    const from = formatter.format(start);
    const to = formatter.format(end);
    const rangeStart =
      first?.year === last?.year
        ? new Intl.DateTimeFormat(undefined, { ...options, year: undefined, timeZone: timezone }).format(start)
        : from;
    const dates = from === to ? from : `${rangeStart}–${to}`;
    return `${reportLabel(period.kind)} · ${dates}`;
  };
  try {
    return format(period.timezone || "UTC");
  } catch {
    return `${format("UTC")} · UTC (report timezone unavailable)`;
  }
};
const reportTitles: Record<string, string> = {
  monthly: "Monthly service report",
  quarterly: "Quarterly service report",
  annual: "Annual performance report",
  ad_hoc: "Requested operational report",
};
export const reportTitle = (kind?: string) => reportTitles[kind ?? ""] ?? "Service report";
export const reportWorkflow = (status: string) => (status === "issued" ? "Published" : reportLabel(status));
export const reportDate = (time?: Timestamp, timezone = "UTC") =>
  formatReportTime(time, timezone, { dateStyle: "medium" });
export const reportDateRange = (period?: Pick<ReportingPeriod, "start" | "end" | "timezone">) =>
  period
    ? `${reportDate(period.start, period.timezone)} – ${reportDate(period.end ? { ...period.end, seconds: period.end.seconds - 1n } : undefined, period.timezone)}`
    : "Reporting period unavailable";

export const reportTimeInput = (time: Timestamp | undefined, timezone: string) => {
  if (!time) return "";
  const parts = getTimeZoneDateTimeParts(new Date(Number(time.seconds) * 1000), timezone);
  if (!parts) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${parts.year}-${pad(parts.month)}-${pad(parts.day)}T${pad(parts.hours)}:${pad(parts.minutes)}`;
};
export const parseReportTime = (value: string, timezone: string) => {
  if (!value) return undefined;
  const [date, time] = value.split("T");
  if (!date || !time) return undefined;
  const candidate = buildDateInTimeZone(date, time, timezone);
  if (!candidate) return undefined;
  // Require a unique wall time when a clock moves backward.
  const desired = new Date(`${value}Z`).getTime();
  for (const days of [-2, -1, 1, 2]) {
    const nearby = new Date(candidate.getTime() + days * 86400000);
    const parts = getTimeZoneDateTimeParts(nearby, timezone);
    if (!parts) return undefined;
    const offset = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hours, parts.minutes) - nearby.getTime();
    const alternate = new Date(desired - offset);
    if (
      alternate.getTime() !== candidate.getTime() &&
      reportTimeInput(timestampFromDate(alternate), timezone) === value
    )
      return undefined;
  }
  return timestampFromDate(candidate);
};

export const reportDueDate = (target?: ReportingTarget) =>
  target?.status === "authoritative" && !target.reasonableEfforts ? target.date || "—" : "—";
