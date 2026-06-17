import { type NavFeature } from "@/protoFleet/config/navItems";
import { useNotificationsEnabled } from "@/protoFleet/features/notifications/api/useNotificationsEnabled";

/**
 * Runtime on/off state for nav features the server gates (see
 * `SecondaryNavItem.requiredFeature`). Shared by the desktop `SecondaryNavigation`
 * and the mobile settings submenu in `Navigation` so both hide the same entries.
 */
export function useNavFeatureEnabled(): Record<NavFeature, boolean> {
  return {
    notifications: useNotificationsEnabled(),
  };
}
