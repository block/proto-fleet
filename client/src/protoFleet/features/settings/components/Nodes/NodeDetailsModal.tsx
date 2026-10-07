import type { ReactNode } from "react";
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

const DetailField = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0 phone:flex-col phone:gap-1">
    <dt className="shrink-0 text-text-primary-50">{label}</dt>
    <dd className="min-w-0 text-right text-text-primary phone:text-left">{children}</dd>
  </div>
);

const NodeDetailsModal = ({ node, canFindMiners, onDismiss, onFindMiners }: NodeDetailsModalProps) => (
  <Modal open onDismiss={onDismiss} title={node.name} testId="node-details-modal">
    <div className="flex flex-col gap-4 text-300">
      <section
        className="rounded-xl border border-border-5 bg-surface-base p-4"
        aria-labelledby="node-connection-heading"
      >
        <h3 id="node-connection-heading" className="mb-3 text-emphasis-300 text-text-primary">
          Connection
        </h3>
        <dl className="divide-y divide-border-5">
          <DetailField label="Status">
            <NodeStatusBadge node={node} />
          </DetailField>
          <DetailField label="Command connection">
            {node.controlStreamConnected ? "Connected" : "Disconnected"}
          </DetailField>
          <DetailField label="Last heartbeat">
            {node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}
          </DetailField>
        </dl>
      </section>
      <section
        className="rounded-xl border border-border-5 bg-surface-base p-4"
        aria-labelledby="node-identity-heading"
      >
        <h3 id="node-identity-heading" className="mb-3 text-emphasis-300 text-text-primary">
          Identity
        </h3>
        <dl className="divide-y divide-border-5">
          <DetailField label="Identity fingerprint">
            <code className="font-mono text-200 break-all">{node.identityFingerprint || "—"}</code>
          </DetailField>
          <DetailField label="Node ID">{node.fleetNodeId}</DetailField>
          <DetailField label="Enrolled">{node.createdAt?.toLocaleString() ?? "—"}</DetailField>
        </dl>
      </section>
      {canFindMiners ? (
        <div className="flex justify-end pt-2">
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Find miners"
            disabled={!node.controlStreamConnected || node.commandProtocolUpgradeRequired}
            onClick={onFindMiners}
            className="phone:w-full"
          />
        </div>
      ) : null}
    </div>
  </Modal>
);

export default NodeDetailsModal;
