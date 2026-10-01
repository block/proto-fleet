import { vi } from "vitest";

import { activeRigRollout, canaryChannel, canaryPreview } from "../ReleaseChannels.fixtures";
import { isActive } from "../rolloutStatus";
import { releaseChannelsApi } from "./helpers";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";

export function apiFor(rollout: Rollout) {
  return {
    ...releaseChannelsApi(),
    channels: [
      {
        ...canaryChannel,
        modelGroups: canaryChannel.modelGroups.map((group) =>
          group.manufacturer === rollout.manufacturer && group.model === rollout.model
            ? {
                ...group,
                assignmentGeneration: rollout.assignmentGeneration,
                firmwareChecksum: rollout.firmwareChecksum,
                activeRolloutId: isActive(rollout) ? rollout.id : 0n,
              }
            : group,
        ),
      },
    ],
    rollouts: [rollout],
    previewScope: vi.fn().mockResolvedValue(canaryPreview),
    listChannelRollouts: vi.fn().mockResolvedValue([rollout]),
  } satisfies ReleaseChannelsApi;
}

// Banner-selection regressions need multiple active updates; a single active
// update now exposes its controls directly on the firmware page.
export function apiWithBanners(rollout: Rollout) {
  const api = apiFor(rollout);
  const other = { ...activeRigRollout, id: 900n, manufacturer: "Other" };
  api.rollouts.push(other);
  const group = api.channels[0].modelGroups.find((candidate) => candidate.model === other.model)!;
  api.channels[0].modelGroups.push({ ...group, manufacturer: other.manufacturer, activeRolloutId: other.id });
  return api;
}
