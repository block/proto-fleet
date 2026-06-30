import { timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type CohortDeviceDisplay,
  type CohortMember,
  CohortState,
  type CohortSummary,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";

export const splitIdentifiers = (value: string) =>
  value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean);

export const formatCohortTimestamp = (timestamp: CohortSummary["expiresAt"]) => {
  if (!timestamp) return "No expiry";
  return new Date(timestampMs(timestamp)).toLocaleString();
};

export const formatDateTimeLocal = (date: Date) => {
  const offsetMs = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offsetMs).toISOString().slice(0, 16);
};

export const parseOptionalDateTimeLocal = (value: string) => {
  const trimmed = value.trim();
  return trimmed ? new Date(trimmed) : undefined;
};

export type ExpiryPreset = "none" | "4h" | "8h" | "24h" | "3d" | "7d" | "custom";
export type ExpiryUnit = "hours" | "days";

const presetDurationMs: Partial<Record<ExpiryPreset, number>> = {
  "4h": 4 * 60 * 60 * 1000,
  "8h": 8 * 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "3d": 3 * 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
};

const unitMs: Record<ExpiryUnit, number> = {
  hours: 60 * 60 * 1000,
  days: 24 * 60 * 60 * 1000,
};

export const durationToExpiresAt = (
  preset: ExpiryPreset,
  customAmount: string,
  customUnit: ExpiryUnit,
  baseDate = new Date(),
) => {
  if (preset === "none") return undefined;

  const durationMs =
    preset === "custom" ? Number.parseFloat(customAmount.trim()) * unitMs[customUnit] : presetDurationMs[preset];

  if (!durationMs || !Number.isFinite(durationMs) || durationMs <= 0) {
    throw new Error("Expiration duration must be greater than zero");
  }

  return new Date(baseDate.getTime() + durationMs);
};

export const cohortStateLabel = (state?: CohortState) => {
  switch (state) {
    case CohortState.ACTIVE:
      return "Active";
    case CohortState.RELEASED:
      return "Released";
    default:
      return "Unknown";
  }
};

export const isActiveNonDefaultCohort = (cohort?: CohortSummary) =>
  Boolean(cohort && !cohort.isDefault && cohort.state === CohortState.ACTIVE);

export const isActiveCohort = (cohort?: CohortSummary) => Boolean(cohort && cohort.state === CohortState.ACTIVE);

export const isAdminRole = (role: string) => {
  const normalized = role.trim().toUpperCase();
  return normalized === "ADMIN" || normalized === "SUPER_ADMIN";
};

export const isSuperAdminRole = (role: string) => role.trim().toUpperCase() === "SUPER_ADMIN";

export const cohortDeviceDisplayName = ({
  deviceIdentifier,
  display,
}: {
  deviceIdentifier: string;
  display?: Partial<CohortDeviceDisplay>;
}) => {
  const name = display?.name?.trim();
  const workerName = display?.workerName?.trim();
  const serialNumber = display?.serialNumber?.trim();
  const modelName = [display?.manufacturer?.trim(), display?.model?.trim()].filter(Boolean).join(" ");

  if (name && name !== modelName) return name;
  return serialNumber || workerName || name || deviceIdentifier;
};

export const cohortDeviceSecondaryText = (display?: Partial<CohortDeviceDisplay>, primaryText?: string) => {
  const primary = primaryText?.trim().toLocaleLowerCase();
  const parts = [
    display?.workerName,
    display?.manufacturer && display?.model ? `${display.manufacturer} ${display.model}` : display?.model,
    display?.ipAddress,
    display?.serialNumber,
  ].filter((part): part is string => Boolean(part));

  return parts.filter((part) => part.trim().toLocaleLowerCase() !== primary).join(" - ");
};

export const cohortMemberSiteLabel = (member: CohortMember) => member.display?.siteLabel || "Unassigned";
