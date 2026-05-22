import clsx from "clsx";

import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import type { SchedulePopoverSection } from "@/protoFleet/components/PageHeader/schedulePillUtils";
import SchedulePopover from "@/protoFleet/components/PageHeader/SchedulePopover";
import { scheduleStatusDotClassName } from "@/protoFleet/features/settings/components/Schedules/constants";

interface SchedulePillProps {
  pillSchedule: ScheduleListItem;
  sections: SchedulePopoverSection[];
  pendingScheduleId: string | null;
  onToggleScheduleStatus: (schedule: ScheduleListItem) => Promise<void>;
}

const SchedulePill = ({ pillSchedule, sections, pendingScheduleId, onToggleScheduleStatus }: SchedulePillProps) => {
  return (
    <PageHeaderPopoverPill
      ariaLabel={`View schedule details for ${pillSchedule.name}`}
      prefixIcon={
        <span className={clsx("h-2.5 w-2.5 rounded-full", scheduleStatusDotClassName[pillSchedule.status])} />
      }
      triggerClassName="schedule-pill-trigger"
      triggerContent={<span className="block max-w-56 truncate">{pillSchedule.name}</span>}
    >
      {({ closePopover }) => (
        <SchedulePopover
          sections={sections}
          pendingScheduleId={pendingScheduleId}
          onToggleScheduleStatus={onToggleScheduleStatus}
          onNavigateToSchedules={closePopover}
        />
      )}
    </PageHeaderPopoverPill>
  );
};

export default SchedulePill;
