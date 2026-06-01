import { useCallback, useEffect, useMemo, useState } from "react";

import { buildSchedulePopoverSections, type SchedulePopoverSection, selectPillSchedule } from "./schedulePillUtils";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useScheduleApiContext } from "@/protoFleet/api/ScheduleApiContext";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import { useRole } from "@/protoFleet/store";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export interface UseSchedulePillDataResult {
  hasVisibleSchedules: boolean;
  pillSchedule: ScheduleListItem | null;
  sections: SchedulePopoverSection[];
  pendingScheduleId: string | null;
  onToggleScheduleStatus: (schedule: ScheduleListItem) => Promise<void>;
}

const POLL_INTERVAL_MS = 30_000;
const ROLES_WITH_SCHEDULE_READ = new Set(["SUPER_ADMIN", "ADMIN"]);

export const useSchedulePillData = (): UseSchedulePillDataResult => {
  const { schedules, refreshSchedules, pauseSchedule, resumeSchedule } = useScheduleApiContext();
  const [pendingScheduleId, setPendingScheduleId] = useState<string | null>(null);
  const role = useRole();
  // ListSchedules is server-side gated on schedule:read. Mirror the same
  // role-name allowlist used by navItems.ts so non-admin sessions don't
  // generate PermissionDenied every poll interval from this globally-
  // mounted hook. Permission-key plumbing for custom roles is a separate
  // follow-up (UserInfo.permissions + client store changes).
  const canReadSchedules = ROLES_WITH_SCHEDULE_READ.has(role);

  useEffect(() => {
    if (!canReadSchedules) {
      return;
    }
    const refreshScheduleSummary = () => {
      void refreshSchedules({ background: true }).catch(() => {});
    };

    refreshScheduleSummary();
    const intervalId = window.setInterval(refreshScheduleSummary, POLL_INTERVAL_MS);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [canReadSchedules, refreshSchedules]);

  const { sections, pillSchedule } = useMemo(() => {
    const nextSections = buildSchedulePopoverSections(schedules);

    return {
      sections: nextSections,
      pillSchedule: selectPillSchedule(nextSections),
    };
  }, [schedules]);

  const onToggleScheduleStatus = useCallback(
    async (schedule: ScheduleListItem) => {
      if (schedule.status === "completed") {
        return;
      }

      setPendingScheduleId(schedule.id);

      try {
        if (schedule.status === "paused") {
          await resumeSchedule(schedule.id);
        } else {
          await pauseSchedule(schedule.id);
        }
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to update schedule"),
          status: STATUSES.error,
        });
      } finally {
        setPendingScheduleId((current) => (current === schedule.id ? null : current));
      }
    },
    [pauseSchedule, resumeSchedule],
  );

  return useMemo(
    () => ({
      hasVisibleSchedules: pillSchedule !== null,
      pillSchedule,
      sections,
      pendingScheduleId,
      onToggleScheduleStatus,
    }),
    [onToggleScheduleStatus, pendingScheduleId, pillSchedule, sections],
  );
};
