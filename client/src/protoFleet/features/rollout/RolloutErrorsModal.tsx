import type { ReactElement } from "react";

import { rolloutErrorImpactCount, rolloutProcessLabel } from "./rolloutDisplayUtils";
import type { RolloutErrorImpact, RolloutEvent } from "./rolloutTypes";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";

interface RolloutErrorsModalProps {
  open: boolean;
  event: RolloutEvent;
  onDismiss: () => void;
}

function impactedMinerLabel(count: number): string {
  return `${count.toLocaleString()} impacted ${count === 1 ? "miner" : "miners"}`;
}

type RolloutErrorColumn = "message" | "impactedMiners";

const rolloutErrorColumns: RolloutErrorColumn[] = ["message", "impactedMiners"];

const rolloutErrorColTitles: ColTitles<RolloutErrorColumn> = {
  message: "Error",
  impactedMiners: "Impacted miners",
};

function TextCell({ value, emphasis = false }: { value: string; emphasis?: boolean }): ReactElement {
  return (
    <span className={emphasis ? "text-emphasis-300 break-words text-text-primary" : "break-words text-text-primary"}>
      {value}
    </span>
  );
}

const rolloutErrorColConfig: ColConfig<RolloutErrorImpact, string, RolloutErrorColumn> = {
  message: {
    component: (error) => <TextCell value={error.message} emphasis />,
    width: "w-[55%]",
    allowWrap: true,
  },
  impactedMiners: {
    component: (error) => <TextCell value={error.impactedMiners.join(", ")} />,
    width: "w-[45%]",
    allowWrap: true,
  },
};

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
      <List<RolloutErrorImpact, string, RolloutErrorColumn>
        activeCols={rolloutErrorColumns}
        colTitles={rolloutErrorColTitles}
        colConfig={rolloutErrorColConfig}
        items={errors}
        itemKey="id"
        total={errors.length}
        itemName={{ singular: "error", plural: "errors" }}
        tableClassName="mb-0 w-full"
        applyColumnWidthsToCells
        stickyFirstColumn={false}
        emptyStateRow={<div className="py-10 text-center text-300 text-text-primary-70">No errors to show.</div>}
      />
    </Modal>
  );
}

export default RolloutErrorsModal;
