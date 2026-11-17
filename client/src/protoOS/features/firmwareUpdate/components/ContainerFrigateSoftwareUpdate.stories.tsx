import type { Decorator, Meta, StoryObj } from "@storybook/react-vite";
import ContainerFrigateSoftwareUpdate from "./ContainerFrigateSoftwareUpdate";
import type {
  FrigateRollout,
  FrigateServerUpdate,
  FrigateUpdatePlan,
  FrigateUpdateRecord,
} from "@/protoOS/api/adapters/frigateServerUpdate";
import { REMOTE_TRIGGER_DISABLED } from "@/protoOS/api/adapters/frigateUpdateMessages";

const deviceId = "storybook-container";
const record: FrigateUpdateRecord = {
  jobId: "6f1c2b7e-0d1a-4c8e-9a51-3f7b2d9e4c10",
  idempotencyKey: "0b4f6a2c-8e1d-4f3a-b7c9-5d2e1a6f8b30",
  targetVersion: "1.3.0",
  packageName: "frigate_1.3.0_linux_arm64.deb",
  instanceIds: ["ct1-a", "ct1-b"],
  fingerprints: { "ct1-a": "a1", "ct1-b": "b2" },
  step: "started",
};
const plan = (blockers: string[] = []): FrigateUpdatePlan => ({
  controllers: [
    { instanceId: "ct1-a", runningVersion: "1.2.0", architecture: "arm64", spoolFreeBytes: 2e9, trustedKeyIds: ["k"] },
    { instanceId: "ct1-b", runningVersion: "1.2.0", architecture: "arm64", spoolFreeBytes: 2e9, trustedKeyIds: ["k"] },
  ],
  blockers,
});
const rollout = (fields: Partial<FrigateRollout> = {}): FrigateRollout => ({
  jobId: record.jobId,
  targetVersion: "1.3.0",
  fromVersion: "1.2.0",
  phase: "installing",
  ended: false,
  actor: "proto-os",
  releaseKeyId: "sha256:4c1d9e0a7b3f",
  retryable: false,
  cancellable: false,
  instances: [
    { instanceId: "ct1-a", phase: "Installed", runningVersion: "1.2.0", lastContact: new Date(), contactStale: false },
    {
      instanceId: "ct1-b",
      phase: "Software stopped",
      runningVersion: "1.2.0",
      lastContact: new Date(),
      contactStale: false,
    },
  ],
  ...fields,
});

const update = (overrides: Partial<FrigateServerUpdate> = {}): FrigateServerUpdate => ({
  readPackage: async ([deb]) => ({
    deb,
    statement: new Uint8Array(),
    signature: new Uint8Array(1),
    version: "1.3.0",
    architecture: "arm64",
    repository: "btc-mining/frigate",
    sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    sizeBytes: deb.size,
    issuedAt: "2026-11-01T00:00:00Z",
  }),
  plan: async () => plan(),
  stage: async (_, planned, progress) => {
    progress(100, planned.fingerprints);
    return planned.fingerprints;
  },
  discard: async () => undefined,
  start: async () => rollout({ phase: "verifying", cancellable: true }),
  status: async () => ({ servedByMaintenance: false }),
  retry: async () => rollout({ phase: "migrating" }),
  cancel: async () => rollout({ phase: "cancelled", ended: true }),
  ...overrides,
});

/** Seeds this browser's recovery record, as a reload during an update would find it. */
const tracked =
  (saved?: FrigateUpdateRecord): Decorator =>
  (Story) => {
    for (const key of Object.keys(localStorage))
      if (key.startsWith(`proto-os:frigate-update:${deviceId}:`)) localStorage.removeItem(key);
    if (saved) localStorage.setItem(`proto-os:frigate-update:${deviceId}:${saved.jobId}`, JSON.stringify(saved));
    return <Story />;
  };

const meta: Meta<typeof ContainerFrigateSoftwareUpdate> = {
  title: "Proto OS/Firmware Update/Container controller software update",
  component: ContainerFrigateSoftwareUpdate,
  parameters: { layout: "centered" },
  args: { deviceId, update: update() },
  decorators: [tracked()],
};
export default meta;
type Story = StoryObj<typeof meta>;

export const ChooseRelease: Story = {};

export const RemoteTriggerOff: Story = {
  args: { update: update({ plan: async () => plan([REMOTE_TRIGGER_DISABLED]) }) },
};

export const RefusedWhileFlashing: Story = {
  decorators: [
    tracked({
      ...record,
      step: "staged",
      notStarted: {
        title:
          "A hashboard or PSU firmware update is running on a controller. Wait for it to finish, then start the update again.",
        detail: "a firmware update is running: ct1-b/3f9a",
        code: "UPDATE_FIRMWARE_ACTIVE",
      },
    }),
  ],
};

export const InstallingWhileFrigateIsStopped: Story = {
  decorators: [tracked(record)],
  args: { update: update({ status: async () => ({ rollout: rollout(), servedByMaintenance: true }) }) },
};

export const FailedAndRetryable: Story = {
  decorators: [tracked(record)],
  args: {
    update: update({
      status: async () => ({
        rollout: rollout({
          phase: "failed",
          ended: true,
          retryable: true,
          problem: { code: "UPDATE_MIGRATE_FAILED", message: "migration exited with status 1" },
          nextStep: "Fix the database, then retry. Frigate stays stopped until the update finishes.",
        }),
        servedByMaintenance: true,
      }),
    }),
  },
};

export const Succeeded: Story = {
  decorators: [tracked(record)],
  args: {
    update: update({
      status: async () => ({
        rollout: rollout({
          phase: "succeeded",
          ended: true,
          instances: rollout().instances.map((instance) => ({
            ...instance,
            phase: "Software started",
            runningVersion: "1.3.0",
          })),
        }),
        servedByMaintenance: false,
      }),
    }),
  },
};
