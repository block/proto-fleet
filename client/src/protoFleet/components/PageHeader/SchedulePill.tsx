import { useState } from "react";
import clsx from "clsx";

import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import type { SchedulePopoverSection } from "@/protoFleet/components/PageHeader/schedulePillUtils";
import SchedulePopover from "@/protoFleet/components/PageHeader/SchedulePopover";
import { scheduleStatusDotClassName } from "@/protoFleet/features/settings/components/Schedules/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { PopoverProvider, popoverSizes, useResponsivePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

interface SchedulePillProps {
  pillSchedule: ScheduleListItem;
  sections: SchedulePopoverSection[];
  pendingScheduleId: string | null;
  onToggleScheduleStatus: (schedule: ScheduleListItem) => Promise<void>;
}

const SchedulePillContent = ({
  pillSchedule,
  sections,
  pendingScheduleId,
  onToggleScheduleStatus,
}: SchedulePillProps) => {
  const [isPopoverOpen, setIsPopoverOpen] = useState(false);
  const { triggerRef } = useResponsivePopover();

  return (
    <div className="schedule-pill-trigger relative" ref={triggerRef}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup={true}
        ariaExpanded={isPopoverOpen}
        ariaLabel={`View schedule details for ${pillSchedule.name}`}
        onClick={(event) => {
          setIsPopoverOpen((current) => !current);

          if (event.detail > 0) {
            event.currentTarget.blur();
          }
        }}
        prefixIcon={
          <span className={clsx("h-2.5 w-2.5 rounded-full", scheduleStatusDotClassName[pillSchedule.status])} />
        }
      >
        <span className="block max-w-56 truncate">{pillSchedule.name}</span>
      </Button>

      {isPopoverOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.small}
          className="!space-y-0 px-4 pt-4 pb-3"
          closePopover={() => setIsPopoverOpen(false)}
          closeIgnoreSelectors={[".schedule-pill-trigger"]}
        >
          <SchedulePopover
            sections={sections}
            pendingScheduleId={pendingScheduleId}
            onToggleScheduleStatus={onToggleScheduleStatus}
            onNavigateToSchedules={() => setIsPopoverOpen(false)}
          />
        </Popover>
      ) : null}
    </div>
  );
};

const SchedulePill = (props: SchedulePillProps) => (
  <PopoverProvider>
    <SchedulePillContent {...props} />
  </PopoverProvider>
);

export default SchedulePill;
