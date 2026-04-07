import { type ReactNode, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { action } from "storybook/actions";

import SchedulePillComponent from "./SchedulePill";
import { buildSchedulePopoverSections, selectPillSchedule } from "./schedulePillUtils";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import PageHeaderComponent from ".";
import {
  DayOfWeek,
  PowerTargetMode,
  RecurrenceFrequency,
  ScheduleTargetType,
  ScheduleType,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import {
  ScheduleAction as ProtoScheduleAction,
  ScheduleSchema,
} from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { Schedule as ProtoSchedule } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import type { ScheduleAction, ScheduleListItem, ScheduleStatus } from "@/protoFleet/api/useScheduleApi";

const HOUR_IN_MS = 60 * 60 * 1000;
const DAY_IN_MS = 24 * HOUR_IN_MS;
const QUARTER_HOUR_IN_MS = 15 * 60 * 1000;

const roundDateToQuarterHour = (date: Date) =>
  new Date(Math.ceil(date.getTime() / QUARTER_HOUR_IN_MS) * QUARTER_HOUR_IN_MS);

const toTimestamp = (date: Date) => ({
  seconds: BigInt(Math.floor(date.getTime() / 1000)),
  nanos: 0,
});

const createTargets = (count: number) =>
  Array.from({ length: count }, (_, index) => ({
    targetType: ScheduleTargetType.MINER,
    targetId: `miner-${index + 1}`,
  }));

const createScheduleListItem = ({
  id,
  name,
  priority,
  status,
  action,
  startTime,
  endTime,
  nextRunAt,
  targetSummary,
  powerTargetMode,
}: {
  id: string;
  name: string;
  priority: number;
  status: ScheduleStatus;
  action: ScheduleAction;
  startTime: string;
  endTime?: string;
  nextRunAt?: Date;
  targetSummary: string;
  powerTargetMode?: PowerTargetMode;
}): ScheduleListItem => {
  const protoAction =
    action === "setPowerTarget"
      ? ProtoScheduleAction.SET_POWER_TARGET
      : action === "sleep"
        ? ProtoScheduleAction.SLEEP
        : ProtoScheduleAction.REBOOT;

  return {
    id,
    priority,
    name,
    targetSummary,
    scheduleSummary: "Story schedule",
    nextRunSummary: nextRunAt ? `Runs on ${nextRunAt.toLocaleString()}` : null,
    action,
    status,
    createdBy: "Storybook",
    rawSchedule: create(ScheduleSchema, {
      id: BigInt(id),
      name,
      action: protoAction,
      actionConfig: powerTargetMode
        ? {
            mode: powerTargetMode,
          }
        : undefined,
      targets: createTargets(3),
      nextRunAt: nextRunAt ? toTimestamp(nextRunAt) : undefined,
      scheduleType: ScheduleType.RECURRING,
      recurrence: {
        frequency: RecurrenceFrequency.WEEKLY,
        daysOfWeek: [DayOfWeek.SATURDAY, DayOfWeek.SUNDAY],
      },
      startDate: "2026-04-07",
      startTime,
      endTime,
      timezone: "UTC",
    }) as ProtoSchedule,
  };
};

const buildStorySchedules = () => {
  const roundedNow = roundDateToQuarterHour(new Date()).getTime();
  return [
    createScheduleListItem({
      id: "1",
      name: "Weekday ramp-up",
      priority: 1,
      status: "running",
      action: "setPowerTarget",
      startTime: "06:00",
      endTime: "22:00",
      targetSummary: "Applies to 3 miners",
      powerTargetMode: PowerTargetMode.MAX,
    }),
    createScheduleListItem({
      id: "2",
      name: "Night shift",
      priority: 2,
      status: "active",
      action: "sleep",
      startTime: "22:00",
      nextRunAt: new Date(roundedNow + 9 * HOUR_IN_MS + 30 * 60 * 1000),
      targetSummary: "Applies to 3 miners",
    }),
    createScheduleListItem({
      id: "3",
      name: "Weekend reboot",
      priority: 3,
      status: "paused",
      action: "reboot",
      startTime: "21:45",
      nextRunAt: new Date(roundedNow + 4 * DAY_IN_MS + 8 * HOUR_IN_MS + 15 * 60 * 1000),
      targetSummary: "Applies to 3 miners",
    }),
  ];
};

const getToggledStatus = (status: ScheduleStatus): ScheduleStatus => {
  switch (status) {
    case "paused":
      return "active";
    case "running":
    case "active":
    default:
      return "paused";
  }
};

const InteractiveSchedulePillStory = () => {
  const [schedules, setSchedules] = useState<ScheduleListItem[]>(() => buildStorySchedules());
  const [pendingScheduleId, setPendingScheduleId] = useState<string | null>(null);
  const sections = buildSchedulePopoverSections(schedules);
  const pillSchedule = selectPillSchedule(sections);

  if (!pillSchedule) {
    throw new Error("Story data is missing a pill schedule");
  }

  const handleToggleScheduleStatus = async (schedule: ScheduleListItem) => {
    const nextStatus = getToggledStatus(schedule.status);
    setPendingScheduleId(schedule.id);
    action("toggle schedule status")(`${schedule.name}: ${schedule.status} -> ${nextStatus}`);
    await new Promise((resolve) => {
      window.setTimeout(resolve, 200);
    });
    setSchedules((currentSchedules) =>
      currentSchedules.map((currentSchedule) =>
        currentSchedule.id === schedule.id ? { ...currentSchedule, status: nextStatus } : currentSchedule,
      ),
    );
    setPendingScheduleId(null);
  };

  return (
    <SchedulePillComponent
      pillSchedule={pillSchedule}
      sections={sections}
      pendingScheduleId={pendingScheduleId}
      onToggleScheduleStatus={handleToggleScheduleStatus}
    />
  );
};

const StoryFrame = ({ children }: { children: ReactNode }) => (
  <div className="flex min-h-[32rem] items-start justify-end bg-surface-base px-16 py-10">{children}</div>
);

const emptySchedulePillData: UseSchedulePillDataResult = {
  hasVisibleSchedules: false,
  pillSchedule: null,
  sections: [],
  pendingScheduleId: null,
  onToggleScheduleStatus: async () => {},
};

export const PageHeader = () => {
  return <PageHeaderComponent schedulePillData={emptySchedulePillData} />;
};

export const SchedulePill = () => {
  return (
    <StoryFrame>
      <InteractiveSchedulePillStory />
    </StoryFrame>
  );
};

export default {
  title: "Proto Fleet/Page Header",
};
