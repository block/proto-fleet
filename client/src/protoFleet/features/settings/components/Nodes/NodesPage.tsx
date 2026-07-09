import { useCallback, useMemo, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import EnrollNodeModal from "@/protoFleet/features/settings/components/Nodes/EnrollNodeModal";
import NodeStatusBadge from "@/protoFleet/features/settings/components/Nodes/NodeStatusBadge";
import RevokeNodeDialog from "@/protoFleet/features/settings/components/Nodes/RevokeNodeDialog";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useHasPermission } from "@/protoFleet/store";
import { Checkmark, Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { usePoll } from "@/shared/hooks/usePoll";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type NodeColumns = "name" | "status" | "fingerprint" | "lastSeen" | "enrolled";

const colTitles: ColTitles<NodeColumns> = {
  name: "Name",
  status: "Status",
  fingerprint: "Fingerprint",
  lastSeen: "Last Seen",
  enrolled: "Enrolled",
};

const activeCols: NodeColumns[] = ["name", "status", "fingerprint", "lastSeen", "enrolled"];

const NodesPage = () => {
  const { listFleetNodes, revokeFleetNode } = useFleetNodes();
  const canReadNodes = useHasPermission("fleetnode:read");
  const canManageNodes = useHasPermission("fleetnode:manage");
  const [nodes, setNodes] = useState<FleetNodeItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showEnrollModal, setShowEnrollModal] = useState(false);
  const [resumeNode, setResumeNode] = useState<FleetNodeItem | null>(null);
  const [revokeNodeData, setRevokeNodeData] = useState<FleetNodeItem | null>(null);
  const [isRevoking, setIsRevoking] = useState(false);
  const notifyNextLoadErrorRef = useRef(true);

  const fetchNodes = useCallback(async () => {
    const notifyError = notifyNextLoadErrorRef.current;
    notifyNextLoadErrorRef.current = false;
    try {
      setNodes(await listFleetNodes());
    } catch (error) {
      if (notifyError) {
        pushToast({
          message: error instanceof Error ? error.message : "Failed to load nodes",
          status: STATUSES.error,
        });
      }
    } finally {
      setIsLoading(false);
    }
  }, [listFleetNodes]);

  // Initial fetch plus polling: last_seen_at (and therefore the derived
  // online/stale status) only moves when the list is refetched. Poll errors
  // stay silent so a blip doesn't spam toasts every interval.
  usePoll({
    fetchData: fetchNodes,
    params: fetchNodes,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: canReadNodes,
  });

  const handleEnrollDismiss = useCallback(() => {
    setShowEnrollModal(false);
    setResumeNode(null);
  }, []);

  const handleNodesUpdated = useCallback(() => {
    void fetchNodes();
  }, [fetchNodes]);

  const handleRevokeConfirm = useCallback(() => {
    if (!revokeNodeData) return;
    setIsRevoking(true);
    void (async () => {
      try {
        await revokeFleetNode(revokeNodeData.fleetNodeId, revokeNodeData.pendingEnrollmentId);
        pushToast({
          message: `Node "${revokeNodeData.name}" has been revoked`,
          status: STATUSES.success,
        });
        setRevokeNodeData(null);
        void fetchNodes();
      } catch (error) {
        pushToast({
          message: error instanceof Error ? error.message : "Failed to revoke node",
          status: STATUSES.error,
        });
      } finally {
        setIsRevoking(false);
      }
    })();
  }, [revokeNodeData, revokeFleetNode, fetchNodes]);

  const availableActions = useMemo(
    () => [
      {
        title: "Confirm enrollment",
        icon: <Checkmark />,
        actionHandler: (node: FleetNodeItem) => {
          setResumeNode(node);
          setShowEnrollModal(true);
        },
        hidden: (node: FleetNodeItem) => node.enrollmentStatus !== FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION,
      },
      {
        title: "Revoke",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: (node: FleetNodeItem) => setRevokeNodeData(node),
      },
    ],
    [],
  );

  const colConfig: ColConfig<FleetNodeItem, string, NodeColumns> = useMemo(
    () => ({
      name: {
        component: (node: FleetNodeItem) => <span className="text-emphasis-300">{node.name}</span>,
        width: "w-48",
      },
      status: {
        component: (node: FleetNodeItem) => <NodeStatusBadge node={node} />,
        width: "w-48",
      },
      fingerprint: {
        component: (node: FleetNodeItem) => (
          <span className="font-mono text-200 text-text-primary-50">{node.identityFingerprint}</span>
        ),
        width: "w-44",
      },
      lastSeen: {
        component: (node: FleetNodeItem) => (
          <span>{node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}</span>
        ),
        width: "w-40",
      },
      enrolled: {
        component: (node: FleetNodeItem) => (
          <span>{node.createdAt ? formatTimestamp(Math.floor(node.createdAt.getTime() / 1000)) : "—"}</span>
        ),
        width: "w-40",
      },
    }),
    [],
  );

  // Redirect callers without fleetnode:read away — placed after all
  // hooks to satisfy rules-of-hooks.
  if (!canReadNodes) {
    return <Navigate to="/settings/network" replace />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader
          title="Nodes"
          description="Hosts running the fleet-node daemon. Each node discovers miners on its network and brokers commands and telemetry between them and Fleet."
        />
        {canManageNodes ? (
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Enroll node"
            onClick={() => setShowEnrollModal(true)}
            className="shrink-0 phone:w-full"
          />
        ) : null}
      </div>

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading nodes...</div>
      ) : (
        <List<FleetNodeItem, string, NodeColumns>
          items={nodes}
          itemKey="fleetNodeId"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          total={nodes.length}
          itemName={{ singular: "node", plural: "nodes" }}
          noDataElement={
            <SettingsEmptyState
              title="No nodes yet"
              description="Enroll a host running the fleet-node daemon to discover and manage the miners on its network."
            />
          }
          actions={canManageNodes ? availableActions : undefined}
        />
      )}

      <EnrollNodeModal
        open={showEnrollModal}
        resumeNode={resumeNode}
        onDismiss={handleEnrollDismiss}
        onUpdated={handleNodesUpdated}
      />
      <RevokeNodeDialog
        open={!!revokeNodeData}
        nodeName={revokeNodeData?.name ?? ""}
        onConfirm={handleRevokeConfirm}
        onDismiss={() => setRevokeNodeData(null)}
        isSubmitting={isRevoking}
      />
    </div>
  );
};

export default NodesPage;
