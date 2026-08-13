import ActiveAlertsPopover from "./ActiveAlertsPopover";
import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { popoverSizes } from "@/shared/components/Popover";

// The header hides this pill as soon as the last alert resolves, so the drill-in it opens is owned above it.
interface ActiveAlertsPillProps {
  groups: ActiveAlertGroup[];
  error: string | null;
  hasMore: boolean;
  onSelectGroup: (group: ActiveAlertGroup) => void;
}

const ActiveAlertsPill = ({ groups, error, hasMore, onSelectGroup }: ActiveAlertsPillProps) => {
  // The header keeps the pill up through a failed poll, so the trigger has to carry that: whatever count it
  // still holds went stale when the poll broke, and with none at all it must not read as a quiet fleet.
  const degraded = error !== null;

  return (
    <PageHeaderPopoverPill
      ariaLabel="View active alerts"
      prefixIcon={
        <Alert
          width={iconSizes.small}
          className={degraded ? "text-intent-critical-fill" : "text-intent-warning-fill"}
        />
      }
      popoverSize={popoverSizes.wide}
      triggerClassName="active-alerts-pill-trigger"
      triggerLabel={groups.length > 0 ? countLabel(groups.length, "active alert") : "Alerts unavailable"}
    >
      {({ closePopover }) => (
        <ActiveAlertsPopover
          groups={groups}
          error={error}
          hasMore={hasMore}
          // Close first: the modal's overlay click would otherwise dismiss a dialog opened behind this panel.
          onSelectGroup={(group) => {
            closePopover();
            onSelectGroup(group);
          }}
          onNavigateToAlerts={closePopover}
        />
      )}
    </PageHeaderPopoverPill>
  );
};

export default ActiveAlertsPill;
