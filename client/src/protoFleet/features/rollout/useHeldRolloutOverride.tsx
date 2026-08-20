import { type ReactElement, useState } from "react";

import HeldRolloutOverrideDialog from "./HeldRolloutOverrideDialog";
import type { RolloutEventEvidence } from "./rolloutTypes";

const HELD_ROLLOUT_OVERRIDE_REASON = "Override held hashrate evidence";

type ContinueHandler = (reason?: string) => void;

export function useHeldRolloutOverride(
  evidence: RolloutEventEvidence | undefined,
  onContinue: ContinueHandler | undefined,
): {
  onContinue: (() => void) | undefined;
  confirmationDialog: ReactElement | null;
} {
  const [isOpen, setIsOpen] = useState(false);
  const heldEvidence = evidence?.status === "held" ? evidence : undefined;

  return {
    onContinue: onContinue
      ? () => {
          if (heldEvidence) {
            setIsOpen(true);
            return;
          }
          onContinue();
        }
      : undefined,
    confirmationDialog:
      isOpen && heldEvidence ? (
        <HeldRolloutOverrideDialog
          evidence={heldEvidence}
          onCancel={() => setIsOpen(false)}
          onConfirm={() => {
            setIsOpen(false);
            onContinue?.(HELD_ROLLOUT_OVERRIDE_REASON);
          }}
        />
      ) : null,
  };
}
