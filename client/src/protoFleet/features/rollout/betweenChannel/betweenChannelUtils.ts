import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { CreateRolloutBatchInput, CreateRolloutMemberInput } from "@/protoFleet/api/useRolloutApi";
import type {
  RolloutLaneReleaseTarget,
  RolloutRecord,
  RolloutStrategy,
} from "@/protoFleet/features/rollout/rolloutTypes";

export type TargetCompatibilityStatus = "compatible" | "missing" | "noOp";

export interface TargetCompatibilityRow {
  key: string;
  manufacturer: string;
  model: string;
  sourceFileId: string;
  sourceVersion: string;
  targetFileId?: string;
  targetFilename?: string;
  targetVersion?: string;
  status: TargetCompatibilityStatus;
}

export interface ManualBatchConfig {
  strategy: Extract<RolloutStrategy, "batched" | "pilotThenContinue">;
  batchSize?: number;
  pilotSize?: number;
}

export function rolloutModelKey(manufacturer: string, model: string): string {
  return `${manufacturer.trim().toLowerCase()}\u0000${model.trim().toLowerCase()}`;
}

export function isActiveRollout(rollout: RolloutRecord | undefined): boolean {
  return Boolean(
    rollout &&
    rollout.state !== "completed" &&
    rollout.state !== "completedWithFailures" &&
    rollout.state !== "aborted" &&
    rollout.state !== "reverted",
  );
}

export function evaluateTargetCompatibility(
  sourceTargets: RolloutLaneReleaseTarget[],
  files: FirmwareFileInfo[],
  selectedFileByModel: Record<string, string>,
): TargetCompatibilityRow[] {
  const fileById = new Map(files.map((file) => [file.id, file]));
  return sourceTargets.map((source) => {
    const key = rolloutModelKey(source.targetManufacturer, source.targetModel);
    const target = fileById.get(selectedFileByModel[key] ?? "");
    const targetVersion = target?.firmware_version?.trim();
    const noOp =
      target !== undefined &&
      (target.id === source.firmwareFileId ||
        (targetVersion !== undefined && targetVersion.length > 0 && targetVersion === source.firmwareVersion));
    return {
      key,
      manufacturer: source.targetManufacturer,
      model: source.targetModel,
      sourceFileId: source.firmwareFileId,
      sourceVersion: source.firmwareVersion,
      targetFileId: target?.id,
      targetFilename: target?.filename,
      targetVersion,
      status: !target ? "missing" : noOp ? "noOp" : "compatible",
    };
  });
}

function members(deviceIdentifiers: string[]): CreateRolloutMemberInput[] {
  return deviceIdentifiers.map((deviceIdentifier) => ({ deviceIdentifier }));
}

export function buildManualBatches(deviceIdentifiers: string[], config: ManualBatchConfig): CreateRolloutBatchInput[] {
  if (deviceIdentifiers.length === 0) {
    return [];
  }
  if (config.strategy === "pilotThenContinue") {
    const pilotSize = Math.min(Math.max(config.pilotSize ?? 0, 0), deviceIdentifiers.length);
    if (pilotSize === 0) {
      return [];
    }
    const batches: CreateRolloutBatchInput[] = [
      { label: "Pilot", members: members(deviceIdentifiers.slice(0, pilotSize)) },
    ];
    if (pilotSize < deviceIdentifiers.length) {
      batches.push({ label: "Remaining", members: members(deviceIdentifiers.slice(pilotSize)) });
    }
    return batches;
  }

  const batchSize = Math.max(config.batchSize ?? 0, 0);
  if (batchSize === 0) {
    return [];
  }
  const batches: CreateRolloutBatchInput[] = [];
  for (let index = 0; index < deviceIdentifiers.length; index += batchSize) {
    batches.push({
      label: `Batch ${batches.length + 1}`,
      members: members(deviceIdentifiers.slice(index, index + batchSize)),
    });
  }
  return batches;
}
