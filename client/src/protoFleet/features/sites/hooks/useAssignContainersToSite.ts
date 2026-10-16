import { useCallback, useRef, useState } from "react";
import { getSiteAssignmentErrorMessage } from "../siteAssignmentErrors";
import { sitesClient } from "@/protoFleet/api/clients";
import { useAuthErrors } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

export function useAssignContainersToSite(onAssigned?: (identifiers: string[], targetSiteId?: bigint) => void) {
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const busy = useRef(false);
  const { handleAuthErrors } = useAuthErrors();
  const bumpSitesRevision = useFleetStore((state) => state.ui.bumpSitesRevision);
  const clearError = useCallback(() => setError(undefined), []);

  const assign = useCallback(
    async (containerIdentifiers: string[], targetSiteId?: bigint) => {
      if (busy.current) return false;
      busy.current = true;
      setSaving(true);
      setError(undefined);
      try {
        await sitesClient.assignContainersToSite({ containerIdentifiers, targetSiteId });
        onAssigned?.(containerIdentifiers, targetSiteId);
        bumpSitesRevision();
        return true;
      } catch (err) {
        handleAuthErrors({ error: err });
        setError(getSiteAssignmentErrorMessage(err));
        return false;
      } finally {
        busy.current = false;
        setSaving(false);
      }
    },
    [bumpSitesRevision, handleAuthErrors, onAssigned],
  );

  return { assign, error, saving, clearError };
}
