import type { ToolConfirmation } from "./types";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface ToolConfirmationCardProps {
  confirmation: ToolConfirmation;
  onResolve: (confirmation: ToolConfirmation, decision: "approve" | "cancel") => void;
}

const ToolConfirmationCard = ({ confirmation, onResolve }: ToolConfirmationCardProps) => {
  const isPending = confirmation.status === "pending";
  const isSubmitting = confirmation.status === "submitting";

  return (
    <section
      aria-labelledby={`tool-confirmation-${confirmation.id}`}
      className="rounded-2xl border border-border-10 bg-surface-base p-4 shadow-100"
      data-testid="tool-confirmation"
    >
      <div className="flex items-start gap-3">
        <span
          aria-hidden="true"
          className="flex size-8 shrink-0 items-center justify-center rounded-full bg-intent-warning-10 text-text-warning"
        >
          !
        </span>
        <div className="min-w-0 flex-1">
          <h3 id={`tool-confirmation-${confirmation.id}`} className="text-emphasis-300 text-text-primary">
            {confirmation.title}
          </h3>
          <p className="mt-1 text-200 text-text-primary-50">{confirmation.description}</p>
        </div>
      </div>

      {confirmation.details.length > 0 ? (
        <dl className="mt-3 divide-y divide-border-5 rounded-xl bg-core-primary-5 px-3">
          {confirmation.details.map((detail) => (
            <div
              key={`${detail.label}-${detail.value}`}
              className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-3 py-2 text-200"
            >
              <dt className="text-text-primary-50">{detail.label}</dt>
              <dd className="text-right break-words text-text-primary">{detail.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}

      {confirmation.error ? (
        <p className="mt-3 text-200 text-text-critical" role="alert">
          {confirmation.error}
        </p>
      ) : null}

      {isPending || isSubmitting ? (
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded-lg border border-border-10 px-3 py-2 text-emphasis-200 text-text-primary outline-none hover:bg-core-primary-5 focus-visible:ring-2 focus-visible:ring-core-primary-20 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={isSubmitting}
            onClick={() => onResolve(confirmation, "cancel")}
          >
            Cancel
          </button>
          <button
            type="button"
            className="flex min-w-24 items-center justify-center gap-2 rounded-lg bg-core-primary-fill px-3 py-2 text-emphasis-200 text-text-contrast outline-none hover:bg-core-primary-80 focus-visible:ring-2 focus-visible:ring-core-primary-20 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={isSubmitting}
            onClick={() => onResolve(confirmation, "approve")}
          >
            {isSubmitting ? <ProgressCircular indeterminate size={14} /> : null}
            {isSubmitting ? "Submitting" : confirmation.confirmLabel}
          </button>
        </div>
      ) : (
        <p className="mt-3 text-emphasis-200 text-text-primary-50" role="status">
          {confirmation.status === "approved" ? "Approved" : null}
          {confirmation.status === "cancelled" ? "Cancelled" : null}
          {confirmation.status === "expired" ? "Confirmation expired" : null}
        </p>
      )}
    </section>
  );
};

export default ToolConfirmationCard;
