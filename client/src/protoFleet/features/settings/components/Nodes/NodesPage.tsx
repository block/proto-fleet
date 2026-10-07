import { useCallback, useMemo, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import EnrollNodeModal from "@/protoFleet/features/settings/components/Nodes/EnrollNodeModal";
import { getNodeDisplayStatus } from "@/protoFleet/features/settings/components/Nodes/getNodeDisplayStatus";
import NodeDetailsModal from "@/protoFleet/features/settings/components/Nodes/NodeDetailsModal";
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

type NodeColumns = "name" | "status" | "miners" | "lastSeen";

const colTitles: ColTitles<NodeColumns> = {
  name: "Name",
  status: "Status",
  miners: "Paired miners",
  lastSeen: "Last Seen",
};

const activeCols: NodeColumns[] = ["name", "status", "miners", "lastSeen"];

const NodesPage = () => {
  const { listFleetNodes, listFleetNodeDevices, revokeFleetNode } = useFleetNodes();
  const canReadNodes = useHasPermission("fleetnode:read");
  const canManageNodes = useHasPermission("fleetnode:manage");
  const canPairMiners = useHasPermission("miner:pair");
  const [nodes, setNodes] = useState<FleetNodeItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showEnrollModal, setShowEnrollModal] = useState(false);
  const [resumeNode, setResumeNode] = useState<FleetNodeItem | null>(null);
  const [detailsNodeId, setDetailsNodeId] = useState<string | null>(null);
  const [blockedPairingByNode, setBlockedPairingByNode] = useState<Record<string, string[]>>({});
  const [revokeNodeData, setRevokeNodeData] = useState<FleetNodeItem | null>(null);
  const [revokeMinerNames, setRevokeMinerNames] = useState<string[]>([]);
  const [revokeImpactLoading, setRevokeImpactLoading] = useState(false);
  const [revokeImpactError, setRevokeImpactError] = useState("");
  const [isRevoking, setIsRevoking] = useState(false);
  const notifyNextLoadErrorRef = useRef(true);
  const revokeImpactRequestRef = useRef(0);

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

  const rememberPairing = useCallback((nodeId: string, identifiers: string[]) => {
    setBlockedPairingByNode((current) => ({
      ...current,
      [nodeId]: [...new Set([...(current[nodeId] ?? []), ...identifiers])],
    }));
  }, []);

  const forgetPairing = useCallback((nodeId: string, identifiers: string[]) => {
    const completed = new Set(identifiers);
    setBlockedPairingByNode((current) => {
      const next = { ...current };
      const remaining = (current[nodeId] ?? []).filter((identifier) => !completed.has(identifier));
      if (remaining.length > 0) next[nodeId] = remaining;
      else delete next[nodeId];
      return next;
    });
  }, []);

  const detailsNode = nodes.find((node) => node.fleetNodeId === detailsNodeId) ?? null;
  const statusCounts = nodes.reduce(
    (counts, node) => {
      const status = getNodeDisplayStatus(node);
      if (status === "connected") counts.connected++;
      if (status === "heartbeatOnly" || status === "upgradeRequired") counts.needsAttention++;
      if (status === "stale" || status === "neverConnected") counts.offline++;
      if (status === "awaitingConfirmation" || status === "pending") counts.pending++;
      if (status === "revoked") counts.revoked++;
      return counts;
    },
    { connected: 0, needsAttention: 0, offline: 0, pending: 0, revoked: 0 },
  );

  const dismissRevoke = useCallback(() => {
    revokeImpactRequestRef.current++;
    setRevokeNodeData(null);
  }, []);

  const openRevoke = useCallback(
    (node: FleetNodeItem) => {
      const requestId = ++revokeImpactRequestRef.current;
      setRevokeNodeData(node);
      setRevokeMinerNames([]);
      setRevokeImpactError("");
      setRevokeImpactLoading(true);
      void listFleetNodeDevices(node.fleetNodeId)
        .then((devices) => {
          if (requestId === revokeImpactRequestRef.current) {
            setRevokeMinerNames(devices.map((device) => device.deviceIdentifier));
          }
        })
        .catch((error) => {
          if (requestId === revokeImpactRequestRef.current) {
            setRevokeImpactError(error instanceof Error ? error.message : "Could not load affected miners.");
          }
        })
        .finally(() => {
          if (requestId === revokeImpactRequestRef.current) setRevokeImpactLoading(false);
        });
    },
    [listFleetNodeDevices],
  );

  const handleRevokeConfirm = useCallback(() => {
    if (!revokeNodeData || revokeImpactLoading || revokeImpactError || isRevoking) return;
    setIsRevoking(true);
    void (async () => {
      try {
        await revokeFleetNode(revokeNodeData.fleetNodeId, revokeNodeData.pendingEnrollmentId);
        pushToast({
          message: `Node "${revokeNodeData.name}" has been revoked`,
          status: STATUSES.success,
        });
        dismissRevoke();
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
  }, [revokeNodeData, revokeImpactLoading, revokeImpactError, isRevoking, revokeFleetNode, fetchNodes, dismissRevoke]);

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
        actionHandler: openRevoke,
      },
    ],
    [openRevoke],
  );

  const colConfig: ColConfig<FleetNodeItem, string, NodeColumns> = useMemo(
    () => ({
      name: {
        component: (node: FleetNodeItem) => <span className="text-emphasis-300">{node.name}</span>,
        width: "w-48",
      },
      status: {
        component: (node: FleetNodeItem) => <NodeStatusBadge node={node} />,
        width: "w-56",
      },
      miners: {
        component: (node: FleetNodeItem) => <span>{node.pairedDeviceCount}</span>,
        width: "w-32",
      },
      lastSeen: {
        component: (node: FleetNodeItem) => (
          <span>{node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}</span>
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

      {!isLoading && nodes.length > 0 ? (
        <div className="grid grid-cols-5 gap-3 phone:grid-cols-2" aria-label="Node status totals">
          {(
            [
              ["Connected", statusCounts.connected],
              ["Needs attention", statusCounts.needsAttention],
              ["Offline", statusCounts.offline],
              ["Pending", statusCounts.pending],
              ["Revoked", statusCounts.revoked],
            ] as const
          ).map(([label, count]) => (
            <div key={label} className="rounded-xl bg-surface-5 p-3">
              <div className="text-200 text-text-primary-50">{label}</div>
              <div className="text-heading-200">{count}</div>
            </div>
          ))}
        </div>
      ) : null}

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
          onRowClick={(node) => setDetailsNodeId(node.fleetNodeId)}
          actions={canManageNodes ? availableActions : undefined}
        />
      )}

      <EnrollNodeModal
        open={showEnrollModal}
        resumeNode={resumeNode}
        onDismiss={handleEnrollDismiss}
        onUpdated={handleNodesUpdated}
      />
      {detailsNode ? (
        <NodeDetailsModal
          key={detailsNode.fleetNodeId}
          node={detailsNode}
          canManage={canManageNodes}
          canPair={canPairMiners}
          blockedPairingIdentifiers={blockedPairingByNode[detailsNode.fleetNodeId] ?? []}
          onDismiss={() => setDetailsNodeId(null)}
          onUpdated={handleNodesUpdated}
          onPairingStarted={(identifiers) => rememberPairing(detailsNode.fleetNodeId, identifiers)}
          onPairingCompleted={(identifiers) => forgetPairing(detailsNode.fleetNodeId, identifiers)}
        />
      ) : null}
      <RevokeNodeDialog
        open={!!revokeNodeData}
        nodeName={revokeNodeData?.name ?? ""}
        affectedMiners={revokeMinerNames}
        isLoadingImpact={revokeImpactLoading}
        impactError={revokeImpactError}
        onConfirm={handleRevokeConfirm}
        onDismiss={() => {
          if (!isRevoking) dismissRevoke();
        }}
        isSubmitting={isRevoking}
      />
    </div>
  );
};

export default NodesPage;
