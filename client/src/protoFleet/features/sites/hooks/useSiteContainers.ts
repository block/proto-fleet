import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { getSiteAssignmentErrorMessage } from "../siteAssignmentErrors";
import { containersClient, sitesClient } from "@/protoFleet/api/clients";
import { ResourceRefSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { ContainerPlacement } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// Containers share the Buildings UI, but membership writes use their typed API.
export function useSiteContainers(enabled: boolean) {
  const [containers, setContainers] = useState<ContainerPlacement[]>();
  const [loadError, setLoadError] = useState<string>();
  const [retryKey, setRetryKey] = useState(0);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const busy = useRef(false);
  const revision = useFleetStore((state) => state.ui.sitesRevision);
  const bumpSitesRevision = useFleetStore((state) => state.ui.bumpSitesRevision);
  const { handleAuthErrors } = useAuthErrors();
  const retry = useCallback(() => {
    setLoadError(undefined);
    setRetryKey((key) => key + 1);
  }, []);
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    void containersClient
      .listContainerPlacements({}, { signal: controller.signal })
      .then(({ containers }) => {
        if (!controller.signal.aborted) {
          setContainers(containers);
          setLoadError(undefined);
        }
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setLoadError(getErrorMessage(err));
      });
    return () => controller.abort();
  }, [enabled, revision, retryKey]);

  const assign = useCallback(
    async (containerIdentifiers: string[], targetSiteId?: bigint) => {
      if (busy.current) return false;
      busy.current = true;
      setSaving(true);
      setError(undefined);
      try {
        await sitesClient.assignContainersToSite({ containerIdentifiers, targetSiteId });
        setContainers((current) =>
          current?.map((container) => {
            if (!containerIdentifiers.includes(container.deviceIdentifier)) return container;
            const site = targetSiteId === undefined ? undefined : create(ResourceRefSchema, { id: targetSiteId });
            return { ...container, site };
          }),
        );
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
    [bumpSitesRevision, handleAuthErrors],
  );

  return { containers, loadError, retry, error, saving, assign };
}
