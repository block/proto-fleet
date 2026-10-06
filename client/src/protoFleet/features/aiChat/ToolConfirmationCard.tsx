import type { ToolConfirmation } from "./types";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import ButtonGroup, { groupVariants, sizes } from "@/shared/components/ButtonGroup";
import Card, { cardType } from "@/shared/components/Card";

interface ToolConfirmationCardProps {
  confirmation: ToolConfirmation;
  onResolve: (confirmation: ToolConfirmation, decision: "approve" | "cancel") => void;
}

const ToolConfirmationCard = ({ confirmation, onResolve }: ToolConfirmationCardProps) => {
  const isPending = confirmation.status === "pending";
  const isSubmitting = confirmation.status === "submitting";

  const buttons = [
    {
      text: "Cancel",
      variant: variants.secondary,
      disabled: isSubmitting,
      onClick: () => onResolve(confirmation, "cancel"),
    },
    {
      text: isSubmitting ? "Submitting" : confirmation.confirmLabel,
      variant: variants.primary,
      disabled: isSubmitting,
      loading: isSubmitting,
      onClick: () => onResolve(confirmation, "approve"),
    },
  ];

  return (
    <Card
      bodyClassName="px-4 pb-4"
      className="border border-border-10"
      headerTone="neutral"
      testId="tool-confirmation"
      title={
        <div className="flex min-w-0 items-center gap-3">
          <span aria-hidden="true" className="shrink-0 text-text-warning">
            <Alert />
          </span>
          <h3 id={`tool-confirmation-${confirmation.id}`} className="min-w-0 text-emphasis-300 text-text-primary">
            {confirmation.title}
          </h3>
        </div>
      }
      type={cardType.default}
    >
      <section aria-labelledby={`tool-confirmation-${confirmation.id}`}>
        <p className="text-200 text-text-primary-50">{confirmation.description}</p>

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
          <ButtonGroup buttons={buttons} className="mt-4" size={sizes.compact} variant={groupVariants.rightAligned} />
        ) : (
          <p className="mt-3 text-emphasis-200 text-text-primary-50" role="status">
            {confirmation.status === "approved" ? "Approved" : null}
            {confirmation.status === "cancelled" ? "Cancelled" : null}
            {confirmation.status === "expired" ? "Confirmation expired" : null}
          </p>
        )}
      </section>
    </Card>
  );
};

export default ToolConfirmationCard;
