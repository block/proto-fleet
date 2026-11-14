import { auditResultLabel, auditResultStatus, formatAuditDetails } from "./auditResult";
import { formatControllerTime } from "./coverage";
import type { AuditEvent } from "@/protoOS/api/adapters/controllerLogs";
import Modal from "@/shared/components/Modal";
import { StatusLabel } from "@/shared/components/StatusCircle";
import SummaryRow from "@/shared/components/SummaryRow";

interface AuditEventDetailModalProps {
  event: AuditEvent | null;
  onDismiss: () => void;
}

const AuditEventDetailModal = ({ event, onDismiss }: AuditEventDetailModalProps) => {
  if (!event) return null;
  const title = event.action || "Audit event";

  return (
    <Modal title={title} dialogLabel={title} manageFocus onDismiss={onDismiss}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col">
          <SummaryRow label="Time">{formatControllerTime(event.time) || "—"}</SummaryRow>
          <SummaryRow label="Actor" className="break-words">
            {event.actor || "—"}
          </SummaryRow>
          <SummaryRow label="Source" className="font-mono break-all">
            {event.source || "—"}
          </SummaryRow>
          <SummaryRow label="Controller" className="font-mono break-all">
            {event.node || "—"}
          </SummaryRow>
          <SummaryRow label="Target" className="break-words">
            {event.target || "—"}
          </SummaryRow>
          <SummaryRow label="Result" divider={Boolean(event.error)}>
            <StatusLabel status={auditResultStatus(event.result)}>{auditResultLabel(event.result)}</StatusLabel>
          </SummaryRow>
          {event.error ? (
            <SummaryRow label="Issue" className="break-words" critical divider={false}>
              {event.error}
            </SummaryRow>
          ) : null}
        </div>

        {event.details ? (
          <div className="flex flex-col gap-2">
            <div className="text-heading-200 text-text-primary">Details</div>
            <pre className="overflow-x-auto rounded-xl bg-core-primary-5 px-4 py-3 font-mono text-200 break-all whitespace-pre-wrap text-text-primary">
              {formatAuditDetails(event.details)}
            </pre>
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default AuditEventDetailModal;
