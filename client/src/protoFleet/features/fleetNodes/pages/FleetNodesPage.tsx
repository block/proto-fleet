import { useCallback, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  FleetNodeEnrollmentStatus,
  type FleetNodeSummary,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import CopyableValue from "@/protoFleet/features/fleetNodes/components/CopyableValue";
import EnrollFleetNodeModal from "@/protoFleet/features/fleetNodes/components/EnrollFleetNodeModal";
import {
  enrollmentStatusLabel,
  enrollmentStatusTone,
  isConnected,
} from "@/protoFleet/features/fleetNodes/utils/fleetNodeStatus";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import List from "@/shared/components/List";
import { type ColConfig, type ColTitles, type ListAction } from "@/shared/components/List/types";
import StatusCircle from "@/shared/components/StatusCircle";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { usePoll } from "@/shared/hooks/usePoll";

type ColKey = "name" | "enrollmentStatus" | "identityFingerprint" | "lastSeenAt";

const colTitles: ColTitles<ColKey> = {
  name: "Name",
  enrollmentStatus: "Status",
  identityFingerprint: "Fingerprint",
  lastSeenAt: "Connection",
};

const FleetNodesPage = () => {
  const navigate = useNavigate();
  const { listFleetNodes, confirmFleetNode, revokeFleetNode } = useFleetNodes();

  const [nodes, setNodes] = useState<FleetNodeSummary[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [enrollOpen, setEnrollOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<FleetNodeSummary | null>(null);
  const [revokePending, setRevokePending] = useState(false);
  const [apiKey, setApiKey] = useState<string | null>(null);

  const fetchNodes = useCallback(
    () =>
      listFleetNodes({
        onSuccess: (rows) => {
          setNodes(rows);
          setError(null);
        },
        onError: (message) => {
          setError(message);
          setNodes((prev) => prev ?? []);
        },
      }),
    [listFleetNodes],
  );
  // Pause while the enroll modal is open: it polls the same listing at a faster
  // cadence, and re-enabling refetches immediately on close.
  usePoll({ fetchData: fetchNodes, poll: true, pollIntervalMs: POLL_INTERVAL_MS, enabled: !enrollOpen });

  const handleConfirm = useCallback(
    (node: FleetNodeSummary) => {
      confirmFleetNode({
        fleetNodeId: node.fleetNodeId,
        onSuccess: (key) => {
          setApiKey(key);
          fetchNodes();
        },
        onError: (message) => pushToast({ message: message || "Failed to confirm fleet node", status: STATUSES.error }),
      });
    },
    [confirmFleetNode, fetchNodes],
  );

  const handleRevoke = useCallback(() => {
    if (!revokeTarget) return;
    setRevokePending(true);
    revokeFleetNode({
      fleetNodeId: revokeTarget.fleetNodeId,
      onSuccess: () => {
        pushToast({ message: `Revoked ${revokeTarget.name}`, status: STATUSES.success });
        fetchNodes();
      },
      onError: (message) => pushToast({ message: message || "Failed to revoke", status: STATUSES.error }),
      onFinally: () => {
        setRevokePending(false);
        setRevokeTarget(null);
      },
    });
  }, [revokeTarget, revokeFleetNode, fetchNodes]);

  const colConfig: ColConfig<FleetNodeSummary, bigint, ColKey> = {
    name: { width: "min-w-48" },
    enrollmentStatus: {
      width: "min-w-48",
      component: (node) => (
        <span className="flex items-center gap-2">
          <StatusCircle status={enrollmentStatusTone(node.enrollmentStatus)} />
          {enrollmentStatusLabel(node.enrollmentStatus)}
        </span>
      ),
    },
    identityFingerprint: {
      width: "min-w-48",
      component: (node) => <span className="font-mono text-200">{node.identityFingerprint}</span>,
    },
    lastSeenAt: {
      width: "min-w-36",
      component: (node) =>
        isConnected(node.lastSeenAt?.seconds) ? (
          <span className="flex items-center gap-2">
            <StatusCircle status="normal" /> Connected
          </span>
        ) : (
          <span className="text-text-primary-50">Offline</span>
        ),
    },
  };

  const actions: ListAction<FleetNodeSummary>[] = [
    {
      title: "Open",
      actionHandler: (node) => navigate(`/fleet-nodes/${node.fleetNodeId}`),
      hidden: (node) => node.enrollmentStatus !== FleetNodeEnrollmentStatus.CONFIRMED,
    },
    {
      title: "Confirm",
      actionHandler: handleConfirm,
      hidden: (node) => node.enrollmentStatus !== FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
    },
    {
      title: "Revoke",
      actionHandler: (node) => setRevokeTarget(node),
      variant: "destructive",
      hidden: (node) => node.enrollmentStatus === FleetNodeEnrollmentStatus.REVOKED,
    },
  ];

  return (
    <div className="flex h-full flex-col">
      <div className="sticky left-0 z-10 flex items-center justify-between gap-4 bg-surface-base px-6 pt-10 laptop:px-10">
        <h1 className="text-heading-300 text-text-primary">Fleet nodes</h1>
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Enroll fleet node"
          onClick={() => setEnrollOpen(true)}
          testId="enroll-fleet-node"
        />
      </div>

      <div className="px-6 pt-6 laptop:px-10">
        {error ? (
          <Callout
            className="mb-4"
            intent="danger"
            prefixIcon={<Alert />}
            title="Couldn't load fleet nodes"
            subtitle={error}
            buttonText="Retry"
            buttonOnClick={fetchNodes}
          />
        ) : null}

        {nodes === undefined ? (
          <div className="text-300 text-text-primary-70">Loading…</div>
        ) : nodes.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border-5 p-10 text-center text-300 text-text-primary-70">
            No fleet nodes yet. Enroll one to discover and pair miners on its local network.
          </div>
        ) : (
          <List<FleetNodeSummary, bigint, ColKey>
            items={nodes}
            itemKey="fleetNodeId"
            activeCols={["name", "enrollmentStatus", "identityFingerprint", "lastSeenAt"]}
            colTitles={colTitles}
            colConfig={colConfig}
            actions={actions}
          />
        )}
      </div>

      <EnrollFleetNodeModal open={enrollOpen} onDismiss={() => setEnrollOpen(false)} onConfirmed={fetchNodes} />

      {revokeTarget ? (
        <Dialog
          open
          title={`Revoke ${revokeTarget.name}?`}
          subtitle="The agent loses access immediately and must re-enroll to reconnect."
          icon={
            <DialogIcon intent="critical">
              <Alert />
            </DialogIcon>
          }
          onDismiss={() => setRevokeTarget(null)}
          buttons={[
            { text: "Cancel", onClick: () => setRevokeTarget(null), variant: variants.secondary },
            { text: "Revoke", onClick: handleRevoke, variant: variants.danger, loading: revokePending },
          ]}
        />
      ) : null}

      {apiKey ? (
        <Dialog
          open
          title="Fleet node confirmed"
          subtitle="Paste this API key into the agent prompt to finish enrollment. It won't be shown again."
          subtitleSize="text-300"
          icon={
            <DialogIcon intent="success">
              <Success />
            </DialogIcon>
          }
          onDismiss={() => setApiKey(null)}
          buttons={[{ text: "Done", onClick: () => setApiKey(null), variant: variants.primary }]}
        >
          <CopyableValue value={apiKey} ariaLabel="Copy API key" />
        </Dialog>
      ) : null}
    </div>
  );
};

export default FleetNodesPage;
