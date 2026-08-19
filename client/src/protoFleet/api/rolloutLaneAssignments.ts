import { create } from "@bufbuild/protobuf";

import { rolloutClient } from "@/protoFleet/api/clients";
import { GetRolloutLaneAssignmentsRequestSchema } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { assertNotAborted } from "@/protoFleet/api/requestErrors";
import type { RolloutLaneAssignment } from "@/protoFleet/features/rollout/rolloutTypes";

export async function getRolloutLaneAssignments(
  deviceIdentifiers: string[],
  signal?: AbortSignal,
): Promise<RolloutLaneAssignment[]> {
  if (deviceIdentifiers.length === 0) {
    return [];
  }
  assertNotAborted(signal);
  const response = await rolloutClient.getRolloutLaneAssignments(
    create(GetRolloutLaneAssignmentsRequestSchema, { deviceIdentifiers }),
    signal ? { signal } : undefined,
  );
  assertNotAborted(signal);
  return response.assignments.map((assignment) => ({
    deviceIdentifier: assignment.deviceIdentifier,
    laneId: assignment.laneId,
    laneLabel: assignment.laneLabel,
  }));
}
