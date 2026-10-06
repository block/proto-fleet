import { useActiveAlertGroups } from "@/protoFleet/features/alerts/api/useActiveAlertGroups";
import { useAlertsEnabled } from "@/protoFleet/features/alerts/api/useAlertsEnabled";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { useHasPermission } from "@/protoFleet/store";

// `hasVisiblePill` is the header's gate: the pill is an alarm, so nothing firing means nothing in the header —
// except a failed poll, which no other surface reports now that the dashboard card is gone.
export interface UseActiveAlertsPillDataResult {
  groups: ActiveAlertGroup[];
  error: string | null;
  hasMore: boolean;
  hasVisiblePill: boolean;
}

interface UseActiveAlertsPillDataOptions {
  enabled?: boolean;
}

// Gates on the read permission and the runtime feature probe, so the poll never runs without the sidecar.
export function useActiveAlertsPillData({
  enabled = true,
}: UseActiveAlertsPillDataOptions = {}): UseActiveAlertsPillDataResult {
  const hasAlertRead = useHasPermission("alert:read");
  const alertsEnabled = useAlertsEnabled();
  const canViewAlerts = hasAlertRead && alertsEnabled;
  const { groups, error, hasMore, denied } = useActiveAlertGroups({ enabled: enabled && canViewAlerts });
  // A revoked grant stops the poll but leaves the last response in state, so drop it rather than show it stale.
  const visible = canViewAlerts && !denied;
  const visibleGroups = visible ? groups : [];
  const visibleError = visible ? error : null;

  return {
    groups: visibleGroups,
    error: visibleError,
    hasMore: visible ? hasMore : false,
    hasVisiblePill: visibleGroups.length > 0 || visibleError !== null,
  };
}
