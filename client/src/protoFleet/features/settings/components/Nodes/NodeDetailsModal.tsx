import NodeStatusBadge from "./NodeStatusBadge";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";

interface NodeDetailsModalProps {
  node: FleetNodeItem;
  canFindMiners: boolean;
  onDismiss: () => void;
  onFindMiners: () => void;
}

const NodeDetailsModal = ({ node, canFindMiners, onDismiss, onFindMiners }: NodeDetailsModalProps) => (
  <Modal open onDismiss={onDismiss} title={node.name} size="large" testId="node-details-modal">
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-2 gap-4 text-300 phone:grid-cols-1">
        <div>
          <div className="text-text-primary-50">Status</div>
          <NodeStatusBadge node={node} />
        </div>
        <div>
          <div className="text-text-primary-50">Last heartbeat</div>
          <div>{node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}</div>
        </div>
        <div>
          <div className="text-text-primary-50">Command connection</div>
          <div>{node.controlStreamConnected ? "Connected" : "Disconnected"}</div>
        </div>
        <div>
          <div className="text-text-primary-50">Identity fingerprint</div>
          <div className="font-mono">{node.identityFingerprint}</div>
        </div>
        <div>
          <div className="text-text-primary-50">Enrolled</div>
          <div>{node.createdAt?.toLocaleString() ?? "—"}</div>
        </div>
        <div>
          <div className="text-text-primary-50">Node ID</div>
          <div>{node.fleetNodeId}</div>
        </div>
      </div>
      {canFindMiners ? (
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Find miners"
          disabled={!node.controlStreamConnected || node.commandProtocolUpgradeRequired}
          onClick={onFindMiners}
          className="self-start"
        />
      ) : null}
    </div>
  </Modal>
);

export default NodeDetailsModal;
