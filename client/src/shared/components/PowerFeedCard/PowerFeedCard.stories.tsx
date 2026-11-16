import type { Meta, StoryObj } from "@storybook/react";
import PowerFeedCard, { type PowerFeedView } from ".";

const healthy: PowerFeedView = {
  feedId: "pdu-1-outlet-05",
  confidence: "wiring_verified",
  reasons: [],
  sharedWith: [],
  recoveryCycle: null,
  reading: {
    state: "enabled",
    active: true,
    faulted: false,
    overridden: false,
    voltageV: 480.2,
    currentA: 21.9,
    powerW: 18_240,
    lineVoltagesV: [480.2, 479.8, 480.4],
    phaseCurrentsA: [21.9, 21.8, 22.0],
  },
};

const meta = {
  title: "Shared/Components/PowerFeedCard",
  component: PowerFeedCard,
  decorators: [(Story) => <div className="max-w-6xl bg-surface-base p-6">{Story()}</div>],
  args: { feed: healthy },
} satisfies Meta<typeof PowerFeedCard>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WiringVerified: Story = {};
export const Configured: Story = { args: { feed: { ...healthy, confidence: "configured" } } };
export const Disabled: Story = {
  args: {
    feed: {
      ...healthy,
      confidence: "configured",
      reading: {
        state: "disabled",
        active: false,
        faulted: false,
        overridden: true,
        voltageV: 0,
        currentA: 0,
        powerW: 0,
      },
    },
  },
};
export const Ambiguous: Story = {
  args: {
    feed: {
      ...healthy,
      confidence: "ambiguous",
      reasons: ["feed_shared", "wiring_mismatch"],
      sharedWith: [{ tank: 1, module: 6 }],
    },
  },
};
export const ReadingUnavailable: Story = {
  args: { feed: { ...healthy, reading: undefined, readingUnavailableReason: "read_failed", recoveryCycle: undefined } },
};
export const NoFeedAssigned: Story = {
  args: { feed: { confidence: "unavailable", reasons: ["no_feed_assigned"], sharedWith: [], recoveryCycle: null } },
};
export const RecoveryCycle: Story = {
  args: {
    feed: {
      ...healthy,
      reading: {
        ...healthy.reading!,
        state: "disabled",
        active: false,
        voltageV: 0,
        currentA: 0,
        powerW: 0,
        lineVoltagesV: undefined,
        phaseCurrentsA: undefined,
      },
      recoveryCycle: {
        phase: "holding",
        outletIds: ["pdu-1-outlet-05"],
        restoreAfter: new Date(Date.now() + 25_000),
        lastError: "",
      },
    },
  },
};
