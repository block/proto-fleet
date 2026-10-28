import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import type { FleetNodeDeviceSummary } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import EnrollNodeModal from "@/protoFleet/features/settings/components/Nodes/EnrollNodeModal";
import FindNodeMinersModal from "@/protoFleet/features/settings/components/Nodes/FindNodeMinersModal";
import NodeDetailsModal from "@/protoFleet/features/settings/components/Nodes/NodeDetailsModal";
import NodeRole from "@/protoFleet/features/settings/components/Nodes/NodeRole";
import NodeStatusBadge from "@/protoFleet/features/settings/components/Nodes/NodeStatusBadge";
import RevokeNodeDialog from "@/protoFleet/features/settings/components/Nodes/RevokeNodeDialog";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useHasPermission } from "@/protoFleet/store";
import { Checkmark } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { usePoll } from "@/shared/hooks/usePoll";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";

type NodeColumns = "name" | "ipAddresses" | "ranges" | "role" | "status" | "version" | "miners" | "lastSeen";
type NodeTableItem = FleetNodeItem & { endsGroup: boolean };

const colTitles: ColTitles<NodeColumns> = {
  name: "Name",
  ipAddresses: "IP address",
  ranges: "Discovery ranges",
  role: "Role",
  status: "Connection",
  version: "Version",
  miners: "Paired miners",
  lastSeen: "Last heartbeat",
};

const activeCols: NodeColumns[] = ["name", "ipAddresses", "ranges", "role", "status", "version", "miners", "lastSeen"];

const NodesPage = () => {
  const { listFleetNodes, listFleetNodeDevices, revokeFleetNode, discoverOnFleetNode } = useFleetNodes();
  const canReadNodes = useHasPermission("fleetnode:read");
  const canManageNodes = useHasPermission("fleetnode:manage");
  const canPairMiners = useHasPermission("miner:pair");
  const [nodes, setNodes] = useState<FleetNodeItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showEnrollModal, setShowEnrollModal] = useState(false);
  const [resumeNode, setResumeNode] = useState<FleetNodeItem | null>(null);
  const [detailsNodeId, setDetailsNodeId] = useState<string | null>(null);
  const [findingNodeId, setFindingNodeId] = useState<string | null>(null);
  const [finderRevisionByNode, setFinderRevisionByNode] = useState<Record<string, number>>({});
  const [blockedPairingByNode, setBlockedPairingByNode] = useState<Record<string, string[]>>({});
  const [revokeNodeData, setRevokeNodeData] = useState<FleetNodeItem | null>(null);
  const [revokeDevices, setRevokeDevices] = useState<FleetNodeDeviceSummary[]>([]);
  const [revokeImpactLoading, setRevokeImpactLoading] = useState(false);
  const [revokeImpactError, setRevokeImpactError] = useState("");
  const [isRevoking, setIsRevoking] = useState(false);
  const notifyNextLoadErrorRef = useRef(true);
  const revokeImpactRequestRef = useRef(0);
  const revokePeerConnectedRef = useRef<boolean | undefined>(undefined);
  const scansByNodeRef = useRef(
    new Map<
      string,
      { promise: Promise<void>; onUpdate: Parameters<typeof discoverOnFleetNode>[2]; controller: AbortController }
    >(),
  );

  useEffect(() => {
    const scans = scansByNodeRef.current;
    return () => {
      for (const scan of scans.values()) scan.controller.abort();
      scans.clear();
    };
  }, []);

  const scanOnNode: typeof discoverOnFleetNode = useCallback(
    (nodeId, request, onUpdate) => {
      const running = scansByNodeRef.current.get(nodeId);
      if (running) {
        running.onUpdate = onUpdate;
        return running.promise;
      }
      const scan = { promise: Promise.resolve(), onUpdate, controller: new AbortController() };
      scan.promise = discoverOnFleetNode(
        nodeId,
        request,
        (update) => scan.onUpdate(update),
        scan.controller.signal,
      ).finally(() => {
        if (scansByNodeRef.current.get(nodeId) === scan) scansByNodeRef.current.delete(nodeId);
      });
      scansByNodeRef.current.set(nodeId, scan);
      return scan.promise;
    },
    [discoverOnFleetNode],
  );

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

  const refreshReopenedFinder = useCallback((nodeId: string) => {
    setFinderRevisionByNode((current) => ({ ...current, [nodeId]: (current[nodeId] ?? 0) + 1 }));
  }, []);

  const detailsNode = nodes.find((node) => node.fleetNodeId === detailsNodeId) ?? null;
  const findingNode = nodes.find((node) => node.fleetNodeId === findingNodeId) ?? null;

  const tableNodes = useMemo(() => {
    const byId = new Map(nodes.map((node) => [node.fleetNodeId, node]));
    const seen = new Set<string>();
    const compareNames = (a: FleetNodeItem, b: FleetNodeItem) =>
      a.name.localeCompare(b.name) || a.fleetNodeId.localeCompare(b.fleetNodeId);
    // Name order keeps pairs and their members stationary when HA roles swap.
    return [...nodes].sort(compareNames).flatMap((node): NodeTableItem[] => {
      if (seen.has(node.fleetNodeId)) return [];
      const peer = byId.get(node.haPeerNodeId ?? "");
      const members = peer?.haPeerNodeId === node.fleetNodeId ? [node, peer].sort(compareNames) : [node];
      return members.map((member, index) => {
        seen.add(member.fleetNodeId);
        return { ...member, endsGroup: index === members.length - 1 };
      });
    });
  }, [nodes]);

  const revokePeerNode = revokeNodeData?.haPeerNodeId
    ? (nodes.find((node) => node.fleetNodeId === revokeNodeData.haPeerNodeId) ?? null)
    : null;

  const dismissRevoke = useCallback(() => {
    revokeImpactRequestRef.current++;
    setRevokeNodeData(null);
  }, []);

  useEffect(() => {
    if (!revokeNodeData || isRevoking) return;
    const currentNode = nodes.find((node) => node.fleetNodeId === revokeNodeData.fleetNodeId);
    if (
      !currentNode ||
      currentNode.haRole !== revokeNodeData.haRole ||
      currentNode.haPeerNodeId !== revokeNodeData.haPeerNodeId ||
      currentNode.haStandbyReady !== revokeNodeData.haStandbyReady ||
      currentNode.pairedDeviceCount !== revokeNodeData.pairedDeviceCount ||
      revokePeerNode?.controlStreamConnected !== revokePeerConnectedRef.current
    ) {
      dismissRevoke();
      pushToast({
        message: "Node details changed. Reopen Revoke to review the current impact.",
        status: STATUSES.error,
      });
    }
  }, [nodes, revokeNodeData, revokePeerNode, isRevoking, dismissRevoke]);

  const openRevoke = useCallback(
    (node: FleetNodeItem) => {
      const requestId = ++revokeImpactRequestRef.current;
      revokePeerConnectedRef.current = nodes.find(
        (peer) => peer.fleetNodeId === node.haPeerNodeId,
      )?.controlStreamConnected;
      setRevokeNodeData(node);
      setRevokeDevices([]);
      setRevokeImpactError("");
      setRevokeImpactLoading(true);
      void listFleetNodeDevices(node.fleetNodeId)
        .then((devices) => {
          if (requestId === revokeImpactRequestRef.current) {
            setRevokeDevices(devices);
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
    [listFleetNodeDevices, nodes],
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
    ],
    [],
  );

  const colConfig: ColConfig<NodeTableItem, string, NodeColumns> = useMemo(
    () => ({
      name: {
        component: (node: NodeTableItem) => (
          <span title={node.name} className="text-emphasis-300">
            {node.name}
          </span>
        ),
        width: "w-48",
      },
      ipAddresses: {
        component: (node: FleetNodeItem) => (
          <span className="break-all" title={node.ipAddresses.join(", ")}>
            {node.ipAddresses.length
              ? `${node.ipAddresses[0]}${node.ipAddresses.length > 1 ? ` +${node.ipAddresses.length - 1}` : ""}`
              : "—"}
          </span>
        ),
        width: "w-44",
        allowWrap: true,
      },
      version: {
        component: (node: FleetNodeItem) => <span className="break-all">{node.version || "—"}</span>,
        width: "w-44",
        allowWrap: true,
      },
      ranges: {
        component: (node: FleetNodeItem) => (
          <span className="break-all" title={node.discoveryRanges?.join(", ")}>
            {!node.controlStreamConnected
              ? "Unavailable"
              : !node.discoveryConfigReported
                ? "Upgrade node to report ranges"
                : node.discoveryRanges?.join(", ") || "Not configured"}
          </span>
        ),
        width: "w-56",
        allowWrap: true,
      },
      role: {
        component: (node: FleetNodeItem) => <NodeRole node={node} />,
        width: "w-28",
      },
      status: {
        component: (node: FleetNodeItem) => <NodeStatusBadge node={node} />,
        width: "w-44",
      },
      miners: {
        component: (node: FleetNodeItem) => <span>{node.pairedDeviceCount}</span>,
        width: "w-32",
        align: "right",
      },
      lastSeen: {
        component: (node: FleetNodeItem) => (
          <span title={node.lastSeenAt?.toLocaleString()}>
            {node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}
          </span>
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
    <div className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader title="Nodes" />
        {canManageNodes ? (
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Set up a node"
            onClick={() => setShowEnrollModal(true)}
            className="shrink-0 phone:w-full"
          />
        ) : null}
      </div>

      <div>
        {isLoading ? (
          <div className="text-center text-text-primary-50">Loading nodes...</div>
        ) : (
          <List<NodeTableItem, string, NodeColumns>
            items={tableNodes}
            itemKey="fleetNodeId"
            activeCols={activeCols}
            colTitles={colTitles}
            colConfig={colConfig}
            showRowDivider={(node) => node.endsGroup}
            total={nodes.length}
            itemName={{ singular: "node", plural: "nodes" }}
            noDataElement={
              <SettingsEmptyState
                title="No nodes yet"
                description="Enroll a host running the fleet-node daemon to discover and manage the miners on its network."
              />
            }
            onRowClick={(node) => setDetailsNodeId(node.fleetNodeId)}
            actions={
              canManageNodes &&
              nodes.some((node) => node.enrollmentStatus === FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION)
                ? availableActions
                : undefined
            }
          />
        )}
      </div>

      <EnrollNodeModal
        open={showEnrollModal}
        resumeNode={resumeNode}
        onDismiss={handleEnrollDismiss}
        onUpdated={handleNodesUpdated}
      />
      {detailsNode ? (
        <NodeDetailsModal
          node={detailsNode}
          canFindMiners={Boolean(canManageNodes)}
          onDismiss={() => setDetailsNodeId(null)}
          onFindMiners={() => {
            setDetailsNodeId(null);
            setFindingNodeId(detailsNode.fleetNodeId);
          }}
          onRevoke={
            canManageNodes
              ? () => {
                  setDetailsNodeId(null);
                  openRevoke(detailsNode);
                }
              : undefined
          }
        />
      ) : null}
      {findingNode ? (
        <FindNodeMinersModal
          key={findingNode.fleetNodeId}
          node={findingNode}
          canPair={Boolean(canPairMiners)}
          blockedPairingIdentifiers={blockedPairingByNode[findingNode.fleetNodeId] ?? []}
          settledPairingRevision={finderRevisionByNode[findingNode.fleetNodeId] ?? 0}
          onDismiss={() => setFindingNodeId(null)}
          onUpdated={handleNodesUpdated}
          onPairingStarted={(identifiers) => rememberPairing(findingNode.fleetNodeId, identifiers)}
          onPairingCompleted={(identifiers) => forgetPairing(findingNode.fleetNodeId, identifiers)}
          onPairingSettledAfterDismiss={() => refreshReopenedFinder(findingNode.fleetNodeId)}
          scanOnNode={scanOnNode}
        />
      ) : null}
      {revokeNodeData ? (
        <RevokeNodeDialog
          open
          nodeName={revokeNodeData.name}
          affectedDevices={revokeDevices}
          haRole={revokeNodeData.haRole}
          haStandbyReady={revokeNodeData.haStandbyReady}
          peerNode={revokePeerNode}
          peerName={revokeNodeData.haPeerNodeName}
          isLoadingImpact={revokeImpactLoading}
          impactError={revokeImpactError}
          onConfirm={handleRevokeConfirm}
          onDismiss={() => {
            if (!isRevoking) dismissRevoke();
          }}
          isSubmitting={isRevoking}
        />
      ) : null}
    </div>
  );
};

export default NodesPage;
