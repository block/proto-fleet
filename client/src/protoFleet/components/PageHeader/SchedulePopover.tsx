import { Link } from "react-router-dom";
import clsx from "clsx";

import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import {
  formatSchedulePopoverRelativeStart,
  getSchedulePopoverActionSummary,
  getSchedulePopoverPowerTargetDetail,
  getSchedulePopoverTargetSummary,
  type SchedulePopoverSection,
} from "@/protoFleet/components/PageHeader/schedulePillUtils";
import Button, { sizes, variants } from "@/shared/components/Button";

interface SchedulePopoverProps {
  sections: SchedulePopoverSection[];
  pendingScheduleId: string | null;
  onToggleScheduleStatus: (schedule: ScheduleListItem) => Promise<void>;
  onNavigateToSchedules: () => void;
}

const SchedulePopover = ({
  sections,
  pendingScheduleId,
  onToggleScheduleStatus,
  onNavigateToSchedules,
}: SchedulePopoverProps) => {
  return (
    <div className="flex flex-col">
      <div className="flex flex-col">
        {sections.map((section, sectionIndex) => (
          <section
            key={section.id}
            className={clsx("flex flex-col gap-2", {
              "border-t border-border-5 pt-3": sectionIndex > 0,
              "pb-3": sectionIndex < sections.length - 1,
            })}
          >
            <div className="text-emphasis-300 text-text-primary-50">{section.title}</div>

            {section.schedules.map((schedule) => {
              const powerTargetDetail = getSchedulePopoverPowerTargetDetail(schedule);
              const relativeStart = section.id === "active" ? formatSchedulePopoverRelativeStart(schedule) : null;

              return (
                <div key={schedule.id} className="flex flex-col gap-4 py-1 first:pt-0 last:pb-0">
                  <div className="min-w-0 space-y-1">
                    <div className="truncate text-heading-100 text-text-primary">{schedule.name}</div>
                    {relativeStart ? (
                      <div className="text-200 leading-snug text-text-primary-50">{relativeStart}</div>
                    ) : null}
                    <div className="text-200 leading-snug text-text-primary-70">
                      {getSchedulePopoverActionSummary(section.id, schedule)}
                    </div>
                    <div className="text-200 leading-snug text-text-primary-70">
                      {getSchedulePopoverTargetSummary(schedule)}
                    </div>
                    {powerTargetDetail ? (
                      <div className="text-200 leading-snug text-text-primary-70">{powerTargetDetail}</div>
                    ) : null}
                  </div>

                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    className="w-full justify-center"
                    text={schedule.status === "paused" ? "Resume" : "Pause"}
                    disabled={pendingScheduleId !== null}
                    loading={pendingScheduleId === schedule.id}
                    onClick={() => {
                      void onToggleScheduleStatus(schedule);
                    }}
                  />
                </div>
              );
            })}
          </section>
        ))}
      </div>

      <div className="mt-3 border-t border-border-5 pt-3">
        <Link
          to="/settings/schedules"
          onClick={onNavigateToSchedules}
          className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
        >
          View all schedules
        </Link>
      </div>
    </div>
  );
};

export default SchedulePopover;
