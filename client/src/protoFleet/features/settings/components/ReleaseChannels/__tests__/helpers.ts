import type { ComponentProps } from "react";
import { type Mocked, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import type ReleaseChannelManageView from "../ReleaseChannelManageView";
import {
  PreviewReleaseChannelScopeResponseSchema,
  type Rollout,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";

type ManageProps = ComponentProps<typeof ReleaseChannelManageView>;

export const manageViewProps = () => ({
  rollouts: [] as Rollout[],
  firmwareFiles: [] as FirmwareFileInfo[],
  minerNames: {},
  previewScope: vi
    .fn<ManageProps["previewScope"]>()
    .mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema)),
  listChannelMiners: vi.fn<ManageProps["listChannelMiners"]>().mockResolvedValue([]),
  listRolloutDevices: vi.fn<ManageProps["listRolloutDevices"]>().mockResolvedValue([]),
  onSave: vi.fn<ManageProps["onSave"]>().mockResolvedValue(undefined),
  onApply: vi.fn<ManageProps["onApply"]>().mockResolvedValue(undefined),
});

export const releaseChannelsApi = (): Mocked<ReleaseChannelsApi> => ({
  channels: [],
  rollouts: [],
  minerNames: {},
  isLoading: false,
  hasLoaded: true,
  error: null,
  refresh: vi.fn().mockResolvedValue(undefined),
  createChannel: vi.fn().mockResolvedValue(undefined),
  updateChannel: vi.fn().mockResolvedValue(undefined),
  deleteChannel: vi.fn().mockResolvedValue(undefined),
  previewScope: vi.fn().mockResolvedValue(create(PreviewReleaseChannelScopeResponseSchema)),
  listChannelMiners: vi.fn().mockResolvedValue([]),
  listChannelRollouts: vi.fn().mockResolvedValue([]),
  listRolloutDevices: vi.fn().mockResolvedValue([]),
  applyFirmware: vi.fn().mockResolvedValue([]),
  rollbackFirmware: vi.fn().mockResolvedValue([]),
  continueRollout: vi.fn().mockResolvedValue(undefined),
  pauseRollout: vi.fn().mockResolvedValue(undefined),
  resumeRollout: vi.fn().mockResolvedValue(undefined),
  cancelRollout: vi.fn().mockResolvedValue(undefined),
  retryFailedDevices: vi.fn().mockResolvedValue(undefined),
});

export function deferred<T = void>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
