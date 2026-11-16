import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import type { AlarmNotice } from "../hooks/usePlantAlarmAction";
import type { PlantAlarmItem } from "../plantAlarms";
import PlantAlarmsPanel from "./PlantAlarmsPanel";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { Severity } from "@/protoFleet/api/generated/errors/v1/errors_pb";

const ago = (seconds: number) => timestampFromDate(new Date(Date.now() - seconds * 1000));
const container = (
  plant: Record<string, unknown> = {},
  controls: Record<string, unknown> = {},
  canControlEquipment = true,
) =>
  create(ContainerSchema, {
    deviceIdentifier: "ct1",
    name: "CT1 North",
    canControlEquipment,
    controls: {
      supported: true,
      observedAt: ago(8),
      alarmMuteSupported: true,
      alarmResetSupported: false,
      ...controls,
    },
    plant: { observedAt: ago(12), alarmActive: true, criticalAlarmActive: false, buzzerActive: true, ...plant },
  });
const alarms: PlantAlarmItem[] = [
  {
    id: "a1",
    name: "Coolant level low",
    severity: Severity.MAJOR,
    confirmedAt: new Date(Date.now() - 12_000),
    retained: false,
  },
  {
    id: "a2",
    name: "Dry cooler fan 2 fault",
    severity: Severity.MINOR,
    confirmedAt: new Date(Date.now() - 12_000),
    retained: false,
  },
];
const idle = { busy: false, submit: () => undefined, dismiss: () => undefined };
const notice = (tone: AlarmNotice["tone"], text: string): AlarmNotice => ({
  identifier: "ct1",
  action: "mute",
  tone,
  text,
});

const meta = {
  title: "Proto Fleet/Containers/Container Alarms",
  component: PlantAlarmsPanel,
  args: { container: container(), alarms, command: idle },
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Container alarm evidence first, then Mute and Reset when the controller advertises them. Mute and Reset each open their own confirmation. Outcomes come from a controller observation newer than the command, never from the write response.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="max-w-4xl bg-surface-base p-8">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof PlantAlarmsPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Real CT1: the buzzer can be muted, alarms clear on their own. */
export const AlarmsActiveMuteOnly: Story = {};

export const BothSupported: Story = {
  args: { container: container({ criticalAlarmActive: true }, { alarmResetSupported: true }) },
};

export const WaitingForReadback: Story = {
  args: { command: { ...idle, busy: true, status: "Waiting for the controller to report the result…" } },
};

export const MuteConfirmed: Story = {
  args: {
    container: container({ buzzerActive: false }),
    command: {
      ...idle,
      notice: notice("success", "Alarm buzzer silenced. Alarms stay active until their cause clears."),
    },
  },
};

export const Unconfirmed: Story = {
  args: {
    command: {
      ...idle,
      notice: notice(
        "warning",
        "Mute sent, but no newer controller reading arrived to confirm it. Check the alarm state before trying again.",
      ),
    },
  },
};

export const Failed: Story = {
  args: { command: { ...idle, notice: notice("error", "Mute not sent. The controller is not the primary.") } },
};

export const NoPermission: Story = {
  args: { container: container({}, {}, false) },
};

export const NoAlarms: Story = {
  args: {
    container: container({ alarmActive: false, criticalAlarmActive: false, buzzerActive: false }),
    alarms: [],
  },
};

export const StaleWithCarriedAlarm: Story = {
  args: {
    container: container({ observedAt: ago(420) }),
    alarms: [
      {
        id: "a1",
        name: "Coolant level low",
        severity: Severity.MAJOR,
        confirmedAt: new Date(Date.now() - 420_000),
        retained: true,
      },
    ],
  },
};

export const AlarmDetailsUnavailable: Story = {
  args: { alarms: [], alarmsFailed: true },
};
