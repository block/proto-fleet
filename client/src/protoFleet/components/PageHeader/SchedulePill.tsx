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
      dotClassName={scheduleStatusDotClassName[pillSchedule.status]}
      triggerClassName="schedule-pill-trigger"
      triggerLabel={pillSchedule.name}
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
