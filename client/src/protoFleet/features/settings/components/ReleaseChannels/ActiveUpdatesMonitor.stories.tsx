import { type ComponentProps, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react-vite";

import ActiveUpdatesMonitor from "./ActiveUpdatesMonitor";
import {
  activeRigRollout,
  batchedRigRollout,
  canaryChannel,
  checksums,
  firmwareFiles,
  gatedRigRollout,
  listRolloutDevicesFixture,
  minerNames,
} from "./ReleaseChannels.fixtures";
import ReleaseChannelsTable from "./ReleaseChannelsTable";
import {
  type Rollout,
  RolloutDeviceCountsSchema,
  RolloutDevicePhase,
  RolloutDeviceSchema,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";

const meta = {
  title: "Proto Fleet/Firmware/Release Channels/Active Monitor",
  component: ActiveUpdatesMonitor,
  parameters: { layout: "fullscreen" },
} satisfies Meta<typeof ActiveUpdatesMonitor>;

export default meta;
type Story = StoryObj<typeof ActiveUpdatesMonitor>;
type MonitorApi = ComponentProps<typeof ActiveUpdatesMonitor>["api"];

const noop = () => {};
const settle = () => Promise.resolve();

// A second hardware model in the same channel, with no telemetry sample yet.
const activeS21Rollout: Rollout = {
  ...activeRigRollout,
  id: 142n,
  manufacturer: "Bitmain",
  model: "S21",
  firmwareFileId: "fw-s21-301",
  firmwareChecksum: checksums.s21301,
  firmwareVersion: "3.0.1",
  previousFirmwareChecksum: "",
  previousFirmwareVersion: "",
  assignmentGeneration: 1n,
  deviceCount: 2,
  deviceCounts: create(RolloutDeviceCountsSchema, { inProgress: 1, queued: 1 }),
  evidence: undefined,
};
const s21Devices = [RolloutDevicePhase.IN_PROGRESS, RolloutDevicePhase.QUEUED].map((phase, index) =>
  create(RolloutDeviceSchema, {
    deviceId: BigInt(201 + index),
    deviceIdentifier: `s21-00${index + 1}`,
    firmwareVersion: "3.0.0",
    phase,
  }),
);

// The monitor and drilldowns use production components. Mutations settle
// locally; these stories show fixed server snapshots, without issuing RPCs.
function FirmwareMonitorPage({ rollouts }: { rollouts: Rollout[] }) {
  const [activeTab, setActiveTab] = useState("files");
  const channels = [
    {
      ...canaryChannel,
      modelGroups: canaryChannel.modelGroups.map((group) => {
        const rollout = rollouts.find(
          (candidate) => candidate.manufacturer === group.manufacturer && candidate.model === group.model,
        );
        return rollout
          ? {
              ...group,
              activeRolloutId: rollout.id,
              assignmentGeneration: rollout.assignmentGeneration,
              firmwareFileId: rollout.firmwareFileId,
              firmwareVersion: rollout.firmwareVersion,
              firmwareChecksum: rollout.firmwareChecksum,
              firmwareAvailable: true,
              firmwareTargetManufacturer: rollout.manufacturer,
              firmwareTargetModel: rollout.model,
            }
          : group;
      }),
    },
  ];
  const api: MonitorApi = {
    channels,
    rollouts,
    acknowledgedRollbacks: [],
    minerNames,
    continueRollout: settle,
    pauseRollout: settle,
    resumeRollout: settle,
    cancelRollout: settle,
    rollbackFirmware: () => Promise.resolve([]),
    retryFailedDevices: () => Promise.resolve(undefined),
    listRolloutDevices: (id) =>
      id === activeS21Rollout.id ? Promise.resolve(s21Devices) : listRolloutDevicesFixture(id),
  };

  return (
    <div className="min-h-screen bg-surface-base p-6 tablet:p-10" data-testid="firmware-monitor-story">
      <div className="flex flex-col gap-6">
        <SettingsPageHeader title="Firmware" />
        <ActiveUpdatesMonitor api={api} onManageChannel={() => setActiveTab("releaseChannels")} />
        <TabStrip activeId={activeTab} onSelect={setActiveTab} ariaLabel="Firmware sections">
          <TabStripItem id="files" label="Files" />
          <TabStripItem id="releaseChannels" label="Release channels" />
        </TabStrip>
        <div className="flex items-center justify-between gap-4">
          {activeTab === "releaseChannels" ? (
            <p className="text-300 text-text-primary-70">
              Group miners into release channels and assign firmware per model.
            </p>
          ) : null}
          <Button
            text={activeTab === "files" ? "Upload firmware" : "Create release channel"}
            variant={variants.primary}
            size={sizes.compact}
            onClick={noop}
          />
        </div>
        {activeTab === "files" ? (
          <List<FirmwareFileInfo, string, "filename" | "model" | "version">
            items={firmwareFiles}
            itemKey="id"
            activeCols={["filename", "model", "version"]}
            colTitles={{ filename: "File name", model: "Target", version: "Version" }}
            colConfig={{
              filename: { component: (file) => file.filename, width: "w-96" },
              model: { component: (file) => `${file.target_manufacturer} ${file.target_model}`, width: "w-48" },
              version: { component: (file) => file.firmware_version, width: "w-36" },
            }}
            total={firmwareFiles.length}
            itemName={{ singular: "file", plural: "files" }}
          />
        ) : (
          <ReleaseChannelsTable channels={channels} rollouts={rollouts} onManage={noop} />
        )}
      </div>
    </div>
  );
}

export const SingleInProgress: Story = {
  name: "Single update in progress",
  render: () => <FirmwareMonitorPage rollouts={[activeRigRollout]} />,
};

export const SinglePilotReview: Story = {
  name: "Single update at pilot review",
  render: () => <FirmwareMonitorPage rollouts={[gatedRigRollout]} />,
};

export const SingleBatchReview: Story = {
  name: "Single update at batch review",
  render: () => <FirmwareMonitorPage rollouts={[batchedRigRollout]} />,
};

export const ConcurrentUpdates: Story = {
  name: "Concurrent model updates",
  render: () => <FirmwareMonitorPage rollouts={[gatedRigRollout, activeS21Rollout]} />,
};
