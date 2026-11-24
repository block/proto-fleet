import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { fleetControllerRecords } from "../controllerRecords";
import { containerDetailPath } from "../useOpenContainerView";
import { containersClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { entityScopeTarget, useSyncScopeToEntity } from "@/protoFleet/hooks/useSyncScopeToEntity";
import { useAuthErrors, useHasPermission } from "@/protoFleet/store";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Header from "@/shared/components/Header";
import ControllerLogs from "@/shared/features/controllerLogs/ControllerLogs";
import { usePoll } from "@/shared/hooks/usePoll";

/** A CT1 container controller's own logs and audit trail, read on demand. */
export default function ControllerLogsPage() {
  const { identifier = "" } = useParams<{ identifier: string }>();
  const { handleAuthErrors } = useAuthErrors();
  const { sites } = useSitesContext();
  const [container, setContainer] = useState<{ key: string; value?: Container }>();
  usePoll({
    enabled: !!identifier,
    params: identifier,
    fetchData: async (signal) => {
      try {
        const response = await containersClient.getContainer({ deviceIdentifier: identifier }, { signal });
        if (!signal.aborted) setContainer({ key: identifier, value: response.container });
      } catch (error) {
        // The breadcrumb falls back to the identifier; the reads report their own errors.
        if (!signal.aborted) handleAuthErrors({ error, onError: () => setContainer({ key: identifier }) });
      }
    },
  });
  const current = container?.key === identifier ? container.value : undefined;
  useSyncScopeToEntity(current ? entityScopeTarget(current.site?.id, sites) : undefined);
  // The audit trail also needs activity read, like Fleet's own activity log.
  const canReadAudit = useHasPermission("activity:read");
  const source = useMemo(() => {
    const records = fleetControllerRecords(identifier, handleAuthErrors);
    return canReadAudit ? records : { ...records, listAuditEvents: undefined };
  }, [identifier, handleAuthErrors, canReadAudit]);
  const name = current?.name || identifier;
  const containerCrumb = { label: name, to: containerDetailPath({ deviceIdentifier: identifier }) };

  return (
    <div className="flex min-w-0 flex-col gap-4 p-4 tablet:p-8" data-testid="controller-logs-page">
      <div className="flex flex-col gap-3">
        <Breadcrumb
          segments={
            current?.site
              ? [
                  { label: "Sites", to: "/fleet/sites" },
                  { label: current.site.label, to: `/sites/${current.site.id}` },
                  containerCrumb,
                  { label: "Controller logs" },
                ]
              : [{ label: "Buildings", to: "/fleet/buildings" }, containerCrumb, { label: "Controller logs" }]
          }
          testId="controller-logs-breadcrumb"
        />
        <Header title={<h1>Controller logs</h1>} titleSize="text-heading-300" />
        <p className="text-300 text-text-primary-70">
          Read from {name}&apos;s controller when you open or refresh this page; Fleet doesn&apos;t store them. The
          audit trail is the controller&apos;s own record, separate from Fleet activity.
        </p>
      </div>
      <div className="-mx-4">
        {/* Keyed so another container starts from an empty, freshly read view. */}
        <ControllerLogs key={identifier} {...source} />
      </div>
    </div>
  );
}
