import { type NavFeature } from "@/protoFleet/config/navItems";
import { useAlertsEnabled } from "@/protoFleet/features/alerts/api/useAlertsEnabled";

/**
 * Runtime on/off state for nav features the server gates (see
 * `SecondaryNavItem.requiredFeature`). Shared by the desktop `SecondaryNavigation`
 * and the mobile settings submenu in `Navigation` so both hide the same entries.
 */
export function useNavFeatureEnabled(): Record<NavFeature, boolean> {
  return {
    alerts: useAlertsEnabled(),
  };
}
