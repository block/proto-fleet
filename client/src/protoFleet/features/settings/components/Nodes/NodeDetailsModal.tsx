import type { ReactNode } from "react";
import NodeStatusBadge from "./NodeStatusBadge";
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
}

const DetailField = ({ label, children }: { label: string; children: ReactNode }) => (
  <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0 phone:flex-col phone:gap-1">
    <dt className="shrink-0 text-text-primary-50">{label}</dt>
    <dd className="min-w-0 text-right break-words text-text-primary phone:text-left">{children}</dd>
  </div>
);

const CopyableIdentity = ({ label, value }: { label: string; value: string }) => (
  <span className="flex items-center justify-end gap-2 phone:justify-start">
    <code className="min-w-0 font-mono text-200 break-all">{value || "Unavailable"}</code>
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
          <DetailField label="Commands">
            {node.controlStreamConnected ? "Connection established" : "Connection unavailable"}
          </DetailField>
          <DetailField label="Last heartbeat">
            <span title={node.lastSeenAt?.toLocaleString()}>
              {node.lastSeenAt ? getRelativeTimeFromEpoch(node.lastSeenAt.getTime()) : "Never"}
            </span>
          </DetailField>
          <DetailField label="Paired miners">{node.pairedDeviceCount}</DetailField>
        </dl>
      </section>
      <section className="rounded-xl border border-border-5 bg-surface-base p-4" aria-labelledby="node-host-heading">
        <h3 id="node-host-heading" className="mb-3 text-emphasis-300 text-text-primary">
          Host
        </h3>
        <p className="mb-3 text-200 text-text-primary-50">
          Last reported by this node. IP addresses may require access to its local network or VPN.
        </p>
        <dl className="divide-y divide-border-5">
          <DetailField label="IP addresses">
            {node.ipAddresses.length
              ? node.ipAddresses.map((ip) => (
                  <div key={ip} className="break-all">
                    {ip}
                  </div>
                ))
              : "Unavailable"}
          </DetailField>
          <DetailField label="Running version">
            <span className="break-all">{node.version || "Unavailable"}</span>
          </DetailField>
        </dl>
      </section>
      <section
        className="rounded-xl border border-border-5 bg-surface-base p-4"
        aria-labelledby="node-enrollment-heading"
      >
        <h3 id="node-enrollment-heading" className="mb-3 text-emphasis-300 text-text-primary">
          Enrollment
        </h3>
        <dl className="divide-y divide-border-5">
          <DetailField label="Enrolled by">{node.enrolledBy || "Unavailable"}</DetailField>
          <DetailField label="Enrolled">{node.enrolledAt?.toLocaleString() ?? "Unavailable"}</DetailField>
        </dl>
      </section>
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
