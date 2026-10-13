import { useEffect, useEffectEvent } from "react";

import { useOptionalFleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout/outletContext";

type DisplayMode = "grid" | "list";

/**
 * Saves a `?display=` deep link as the tab's stored view-mode preference so
 * it survives navigating away. A display supplied by the active saved view
 * stays scoped to that view. Only a change in the URL's display value is
 * persisted, so deleting the active view (which leaves its `display` in the
 * URL) does not adopt the deleted view's mode either.
 */
export const usePersistDisplayDeepLink = (
  urlMode: DisplayMode | undefined,
  setStoredMode: (mode: DisplayMode) => void,
) => {
  const activeSavedViewId = useOptionalFleetOutletContext()?.activeSavedViewId;

  const persistUnlessSavedView = useEffectEvent((mode: DisplayMode) => {
    if (activeSavedViewId === undefined) setStoredMode(mode);
  });

  useEffect(() => {
    if (urlMode !== undefined) persistUnlessSavedView(urlMode);
  }, [urlMode]);
};
