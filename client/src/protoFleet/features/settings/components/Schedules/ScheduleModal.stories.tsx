import { useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import {
  DayOfWeek,
  PowerTargetConfigSchema,
  PowerTargetMode,
  RecurrenceFrequency,
  ScheduleAction,
  ScheduleRecurrenceSchema,
  ScheduleSchema,
  ScheduleTargetSchema,
  ScheduleTargetType,
  ScheduleType,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import ScheduleModal from "@/protoFleet/features/settings/components/Schedules/ScheduleModal";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

const createAsyncAction =
  (name: string) =>
  async (...args: unknown[]) => {
    action(name)(...args);
  };

const editSchedule: ScheduleListItem = {
  id: "42",
  priority: 1,
  name: "Weekday ramp-up",
  targetSummary: "Applies to 1 rack and 1 miner",
  scheduleSummary: "Weekdays · 6:00 AM - 10:00 PM",
  nextRunSummary: "Runs tomorrow at 6:00 AM",
  action: "setPowerTarget",
  status: "active",
  createdBy: "admin@fleet.io",
  rawSchedule: create(ScheduleSchema, {
    id: 42n,
    name: "Weekday ramp-up",
    action: ScheduleAction.SET_POWER_TARGET,
    actionConfig: create(PowerTargetConfigSchema, {
      mode: PowerTargetMode.MAX,
    }),
    scheduleType: ScheduleType.RECURRING,
    recurrence: create(ScheduleRecurrenceSchema, {
      frequency: RecurrenceFrequency.WEEKLY,
      interval: 1,
      daysOfWeek: [DayOfWeek.MONDAY, DayOfWeek.TUESDAY, DayOfWeek.WEDNESDAY, DayOfWeek.THURSDAY, DayOfWeek.FRIDAY],
    }),
    startDate: "2026-04-03",
    startTime: "06:00",
    endTime: "22:00",
    timezone: "America/Toronto",
    targets: [
      create(ScheduleTargetSchema, {
        targetType: ScheduleTargetType.RACK,
        targetId: "rack-1",
      }),
      create(ScheduleTargetSchema, {
        targetType: ScheduleTargetType.MINER,
        targetId: "miner-9",
      }),
    ],
  }),
};

type ScheduleModalStoryProps = {
  infoMessage: string;
  schedule?: ScheduleListItem;
};

const ScheduleModalStory = ({ infoMessage, schedule }: ScheduleModalStoryProps) => {
  const [open, setOpen] = useState(true);

  if (!open) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-base">
        <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-surface-base p-4">
      <div className="mb-4 max-w-3xl rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">{infoMessage}</div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <ScheduleModal
        open
        schedule={schedule}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        onCreateSchedule={createAsyncAction("createSchedule")}
        onUpdateSchedule={createAsyncAction("updateSchedule")}
        onDeleteSchedule={createAsyncAction("deleteSchedule")}
        onPauseSchedule={createAsyncAction("pauseSchedule")}
        onResumeSchedule={createAsyncAction("resumeSchedule")}
      />
    </div>
  );
};

const meta = {
  title: "Proto Fleet/Settings/ScheduleModal",
  component: ScheduleModalStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof ScheduleModalStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const AddModal: Story = {
  args: {
    infoMessage:
      "Create-mode schedule modal. Fill in the form to validate the empty-state, preview panel, and save gating.",
  },
};

export const EditModal: Story = {
  args: {
    schedule: editSchedule,
    infoMessage:
      "Edit-mode schedule modal with a recurring weekday power-target window and explicit rack/miner targets.",
  },
};
