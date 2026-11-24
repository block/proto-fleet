import { Code } from "@connectrpc/connect";
import {
  isWireMillivolts,
  MODULE_VOLTAGE_OUTCOME_UNKNOWN,
  type ModuleVoltageReadiness,
  type ModuleVoltageSetting,
  type ModuleVoltageWriteResult,
} from "../moduleVoltage";
import type { AdapterRequestOptions } from "../types";
import type { MdkV2Client } from "./connectClient";
import {
  type V2BoardResult,
  type V2ClearModuleFixedVoltageResponse,
  V2ModuleVoltageReadiness,
  type V2ModuleVoltageSetting,
  type V2SetModuleFixedVoltageResponse,
} from "./generated/generatedApi";
import { ct1ReportedModuleId } from "./normalize";
import { containerWriteError } from "./writeErrors";

const READINESS: Record<string, ModuleVoltageReadiness> = {
  [V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_READY]: "ready",
  [V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_LIMITS_UNAVAILABLE]: "limitsUnavailable",
  [V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_NO_MEMBERS]: "noMembers",
  [V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_TARGET_OUTSIDE_LIMITS]: "targetOutsideLimits",
  [V2ModuleVoltageReadiness.MODULE_VOLTAGE_READINESS_NO_COMMON_RANGE]: "noCommonRange",
};

// The controller reports a range only once every member board's firmware limits are known and overlap.
const RANGE_READINESS = new Set<ModuleVoltageReadiness>(["ready", "targetOutsideLimits"]);

const toSetting = (wire: V2ModuleVoltageSetting): ModuleVoltageSetting | undefined => {
  const moduleId = wire.moduleId?.trim();
  if (!moduleId) return undefined;
  const readiness = (wire.readiness && READINESS[wire.readiness]) || "unknown";
  const minimum = Number(wire.supportedMinimumMillivolts ?? 0);
  const maximum = Number(wire.supportedMaximumMillivolts ?? 0);
  // A zero or inverted range is the wire's way of saying there is none; never present it as a range.
  const supportedRange =
    RANGE_READINESS.has(readiness) && isWireMillivolts(minimum) && isWireMillivolts(maximum) && minimum <= maximum
      ? { minimumMillivolts: minimum, maximumMillivolts: maximum }
      : undefined;
  const fixed = wire.fixedMillivolts === undefined ? undefined : Number(wire.fixedMillivolts);
  const updatedAt = wire.updatedAt ? new Date(wire.updatedAt) : undefined;
  return {
    moduleId,
    // Presence, not value, selects fixed mode; a present zero is still a fixed target the UI must not hide.
    ...(fixed === undefined || !Number.isFinite(fixed) ? {} : { fixedMillivolts: fixed }),
    ...(supportedRange ? { supportedRange } : {}),
    boardCount: wire.boardCount ?? 0,
    readiness,
    ...(wire.readinessMessage ? { readinessMessage: wire.readinessMessage } : {}),
    ...(wire.updatedBy ? { updatedBy: wire.updatedBy } : {}),
    ...(updatedAt && !Number.isNaN(updatedAt.getTime()) ? { updatedAt } : {}),
  };
};

const toWriteResult = (
  data: V2SetModuleFixedVoltageResponse | V2ClearModuleFixedVoltageResponse,
): ModuleVoltageWriteResult => {
  const setting = data.setting ? toSetting(data.setting) : undefined;
  return {
    ...(setting ? { setting } : {}),
    boardsUpdated: data.boardsUpdated ?? 0,
    boards: (data.results ?? []).map((result: V2BoardResult) => ({
      macAddress: result.macAddress || "Unknown board",
      // A plain proto3 bool: JSON gateways omit it when false.
      applied: result.applied === true,
      ...(result.message ? { message: result.message } : {}),
    })),
  };
};

// Codes the controller (or a relay in front of it) returns before it saves anything: a malformed or
// out-of-range request, firmware limits it cannot validate, a module it no longer knows, or a caller
// without the right. Anything else may have saved the setting, so its outcome is unknown until re-read.
const REFUSAL_CODES = new Set<number>([
  Code.InvalidArgument,
  Code.FailedPrecondition,
  Code.NotFound,
  Code.Unimplemented,
  Code.PermissionDenied,
]);

const writeError = (error: unknown) =>
  containerWriteError(error, "The voltage setting was not changed.", MODULE_VOLTAGE_OUTCOME_UNKNOWN, REFUSAL_CODES);

const wireModuleId = (moduleId: string) => {
  const reported = ct1ReportedModuleId(moduleId);
  if (!reported) {
    throw Object.assign(
      new Error("The controller has not reported an identity for this module. The voltage setting was not changed."),
      { writeOutcome: "refused" as const },
    );
  }
  return reported;
};

export async function listModuleVoltageSettings(client: MdkV2Client, params: AdapterRequestOptions) {
  const { data } = await client.miningServiceListModuleVoltageSettings(params);
  return (data.settings ?? []).flatMap((wire) => toSetting(wire) ?? []);
}

export async function writeModuleVoltage(
  client: MdkV2Client,
  moduleId: string,
  millivolts: number | undefined,
  params: AdapterRequestOptions,
) {
  const id = wireModuleId(moduleId);
  if (millivolts !== undefined && !isWireMillivolts(millivolts)) {
    throw Object.assign(new Error("Enter a whole number of millivolts. The voltage setting was not changed."), {
      writeOutcome: "refused" as const,
    });
  }
  try {
    const { data } =
      millivolts === undefined
        ? await client.miningServiceClearModuleFixedVoltage({ moduleId: id }, params)
        : await client.miningServiceSetModuleFixedVoltage({ moduleId: id }, { millivolts }, params);
    return toWriteResult(data);
  } catch (error) {
    if (params.signal?.aborted) throw error;
    throw writeError(error);
  }
}
