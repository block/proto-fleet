import type { ReactNode } from "react";
import Modal from "@/shared/components/Modal";

export function PoolUpdateResults({ children, onDismiss }: { children: ReactNode; onDismiss?: () => void }) {
  return (
    <Modal
      open
      title="Pool configuration"
      dialogLabel="Pool configuration"
      manageFocus
      initialFocus="dialog"
      onDismiss={onDismiss}
    >
      <div className="flex flex-col gap-4">{children}</div>
    </Modal>
  );
}

export function PoolUpdateResultSection({
  name,
  configurationName,
  status,
  explanation,
  requestedAt,
  requestedAtLabel = "Update requested",
  verifiedAt,
  comparison,
  actions,
}: {
  name: string;
  configurationName?: string;
  status: string;
  explanation: string;
  requestedAt?: Date;
  requestedAtLabel?: string;
  verifiedAt?: Date;
  comparison?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <section aria-label={name} className="flex flex-col gap-6 border-b border-border-10 pb-6 last:border-b-0 last:pb-0">
      <div>
        <h2 className="text-heading-200 [overflow-wrap:anywhere]">{name}</h2>
        {configurationName !== undefined ? (
          <p className="mt-1 text-300 text-text-primary-50">
            Configuration: <span className="text-text-primary">{configurationName}</span>
          </p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        <h3 className="text-heading-200">{status}</h3>
        <p className="text-300 text-text-primary-70">{explanation}</p>
      </div>
      {comparison}
      {requestedAt || verifiedAt ? (
        <div className="text-200">
          <h3 className="text-emphasis-200 text-text-primary-70">Details</h3>
          <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-text-primary-50">
            {requestedAt ? (
              <>
                <dt>{requestedAtLabel}</dt>
                <dd>{requestedAt.toLocaleString()}</dd>
              </>
            ) : null}
            {verifiedAt ? (
              <>
                <dt>Last verified</dt>
                <dd>{verifiedAt.toLocaleString()}</dd>
              </>
            ) : null}
          </dl>
        </div>
      ) : null}
      {actions}
    </section>
  );
}
