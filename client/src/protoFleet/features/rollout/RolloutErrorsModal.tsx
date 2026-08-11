import type { ReactElement } from "react";

import { rolloutErrorImpactCount, rolloutProcessLabel } from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import Modal from "@/shared/components/Modal";

interface RolloutErrorsModalProps {
  open: boolean;
  event: RolloutEvent;
  onDismiss: () => void;
}

function impactedMinerLabel(count: number): string {
  return `${count.toLocaleString()} impacted ${count === 1 ? "miner" : "miners"}`;
}

function RolloutErrorsModal({ open, event, onDismiss }: RolloutErrorsModalProps): ReactElement | null {
  if (!open) {
    return null;
  }

  const errors = event.performance?.errors ?? [];
  const impactedMinerCount = rolloutErrorImpactCount(errors);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={`Errors in ${rolloutProcessLabel(event.processType).toLowerCase()}`}
      description={impactedMinerLabel(impactedMinerCount)}
      size="large"
      surfaceClassName="max-w-[760px]"
      bodyClassName="text-text-primary"
      testId="rollout-errors-modal"
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: onDismiss,
          dismissModalOnClick: false,
        },
      ]}
    >
      {errors.length === 0 ? (
        <div className="py-10 text-center text-300 text-text-primary-70">No errors to show.</div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border-5">
          <div className="hidden grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] gap-4 border-b border-border-5 bg-core-primary-5 px-4 py-3 text-200 text-text-primary-50 tablet:grid">
            <div>Error</div>
            <div>Impacted miners</div>
          </div>
          <div className="divide-y divide-border-5">
            {errors.map((error) => {
              const impactedCount = error.impactedMiners.length;
              return (
                <div
                  key={error.id}
                  className="grid gap-3 px-4 py-4 tablet:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] tablet:gap-4"
                >
                  <div className="min-w-0">
                    <div className="text-200 text-text-primary-50 tablet:hidden">Error</div>
                    <div className="mt-1 text-emphasis-300 break-words text-text-primary tablet:mt-0">
                      {error.message}
                    </div>
                    <div className="mt-1 text-200 text-text-primary-50">{impactedMinerLabel(impactedCount)}</div>
                  </div>
                  <div className="min-w-0">
                    <div className="text-200 text-text-primary-50 tablet:hidden">Impacted miners</div>
                    <div className="mt-2 flex flex-wrap gap-2 tablet:mt-0">
                      {error.impactedMiners.map((miner) => (
                        <span
                          key={miner}
                          className="max-w-full rounded-full bg-core-primary-5 px-2 py-1 text-200 break-all text-text-primary"
                          title={miner}
                        >
                          {miner}
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </Modal>
  );
}

export default RolloutErrorsModal;
