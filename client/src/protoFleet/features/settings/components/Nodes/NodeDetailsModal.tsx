import type { ReactNode } from "react";
import NodeRole from "./NodeRole";
import NodeStatusBadge from "./NodeStatusBadge";
import { FleetNodeHARole } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { Copy } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";
import { copyToClipboard } from "@/shared/utils/utility";

interface NodeDetailsModalProps {
  node: FleetNodeItem;
  canFindMiners: boolean;
  onDismiss: () => void;
  onFindMiners: () => void;
  onRevoke?: () => void;
}

const DetailField = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0 phone:flex-col phone:gap-1">
    <dt className="shrink-0 text-text-primary-50">{label}</dt>
    <dd className="min-w-0 text-right break-words text-text-primary phone:text-left">{children}</dd>
  </div>
);

const CopyableIdentity = ({ label, value }: { label: string; value: string }) => (
  <span className="flex items-center justify-end gap-2 phone:justify-start">
    <code className="min-w-0 font-mono text-200 break-all">{value || "—"}</code>
    {value ? (
      <Button
        variant={variants.ghost}
        prefixIcon={<Copy />}
        ariaLabel={`Copy ${label.toLowerCase()}`}
        onClick={() => {
          void copyToClipboard(value)
            .then(() => pushToast({ message: `${label} copied to clipboard`, status: STATUSES.success }))
            .catch(() => pushToast({ message: `Failed to copy ${label.toLowerCase()}`, status: STATUSES.error }));
        }}
      />
    ) : null}
  </span>
);

const NodeDetailsModal = ({ node, canFindMiners, onDismiss, onFindMiners, onRevoke }: NodeDetailsModalProps) => {
  const isHaMember = node.haRole !== FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED;
  const isStandby = node.haRole === FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY;
  return (
    <Modal open onDismiss={onDismiss} title={node.name} testId="node-details-modal">
      <div className="flex flex-col gap-4 text-300">
        <dl className="divide-y divide-border-5">
          <DetailField label="Role">
            <NodeRole node={node} />
          </DetailField>
          <DetailField label="Connection">
            <NodeStatusBadge node={node} />
          </DetailField>
          <DetailField label="Last heartbeat">
            <span title={node.lastSeenAt?.toLocaleString()}>
              {node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}
            </span>
          </DetailField>
          <DetailField label="Paired miners">{node.pairedDeviceCount}</DetailField>
          <DetailField label="IP addresses">
            {node.ipAddresses.length
              ? node.ipAddresses.map((ip) => (
                  <div key={ip} className="break-all">
                    {ip}
                  </div>
                ))
              : "—"}
          </DetailField>
          <DetailField label="Version">
            <span className="break-all">{node.version || "—"}</span>
          </DetailField>
          <DetailField label="Enrolled by">{node.enrolledBy || "—"}</DetailField>
          <DetailField label="Registered">{node.createdAt?.toLocaleString() ?? "—"}</DetailField>
        </dl>
        {isHaMember ? (
          <dl className="divide-y divide-border-5" data-testid="node-ha-details">
            <DetailField label={isStandby ? "Standby for" : "Standby node"}>
              {node.haPeerNodeName || node.haPeerNodeId || "—"}
            </DetailField>
            {node.haStatusReason ? <DetailField label="HA status">{node.haStatusReason}</DetailField> : null}
          </dl>
        ) : null}
        <details className="rounded-xl border border-border-5 bg-surface-base p-4">
          <summary className="cursor-pointer text-emphasis-300 text-text-primary">Technical details</summary>
          <dl className="mt-3 divide-y divide-border-5">
            <DetailField label="Identity fingerprint">
              <CopyableIdentity label="Identity fingerprint" value={node.identityFingerprint} />
            </DetailField>
            <DetailField label="Node ID">
              <CopyableIdentity label="Node ID" value={node.fleetNodeId} />
            </DetailField>
          </dl>
        </details>
        {canFindMiners || onRevoke ? (
          <div className="flex items-center justify-end gap-3 pt-2 phone:flex-col phone:items-stretch">
            {onRevoke ? (
              <Button
                variant={variants.secondaryDanger}
                size={sizes.compact}
                text="Revoke"
                onClick={onRevoke}
                className="mr-auto phone:mr-0 phone:w-full"
              />
            ) : null}
            {canFindMiners ? (
              <>
                {isStandby ? (
                  <span className="text-200 text-text-primary-50">Standby nodes can't find miners.</span>
                ) : null}
                <Button
                  variant={variants.primary}
                  size={sizes.compact}
                  text="Find miners"
                  disabled={!node.controlStreamConnected || node.commandProtocolUpgradeRequired || isStandby}
                  onClick={onFindMiners}
                  className="phone:w-full"
                />
              </>
            ) : null}
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default NodeDetailsModal;
