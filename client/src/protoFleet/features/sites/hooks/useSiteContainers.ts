import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { useAssignContainersToSite } from "./useAssignContainersToSite";
import { containersClient } from "@/protoFleet/api/clients";
import { ResourceRefSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { ContainerPlacement } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

// Containers share the Buildings UI, but membership writes use their typed API.
export function useSiteContainers(enabled: boolean) {
  const [containers, setContainers] = useState<ContainerPlacement[]>();
  const [loadError, setLoadError] = useState<string>();
  const [retryKey, setRetryKey] = useState(0);
  const revision = useFleetStore((state) => state.ui.sitesRevision);
  const updatePlacement = useCallback((containerIdentifiers: string[], targetSiteId?: bigint) => {
    setContainers((current) =>
      current?.map((container) => {
        if (!containerIdentifiers.includes(container.deviceIdentifier)) return container;
        const site = targetSiteId === undefined ? undefined : create(ResourceRefSchema, { id: targetSiteId });
        return { ...container, site };
      }),
    );
  }, []);
  const { assign, error, saving } = useAssignContainersToSite(updatePlacement);
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

  return { containers, loadError, retry, error, saving, assign };
}
