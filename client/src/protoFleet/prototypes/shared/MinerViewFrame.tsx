/**
 * Chrome around a rendered single-miner view.
 *
 * The miner view itself is the clean canvas (`children`). This frame adds only a
 * thin top bar: an optional left action to step back to the picker, and a
 * right-aligned "Details" trigger that tucks the informational chrome (identity
 * + data path) into a modal so it never crowds the actual view.
 */
import { ReactNode, useState } from "react";

import { ArrowLeftCompact, Info } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

export interface MinerViewFrameProps {
  /** Short label for what's mounted, shown in the top bar. */
  title: string;
  /** Optional "back to the picker" affordance (list / connect form). */
  leftAction?: { label: string; onClick: () => void };
  /** Informational chrome shown behind the "Details" trigger. */
  details?: ReactNode;
  /** The clean single-miner-view canvas. */
  children: ReactNode;
}

export function MinerViewFrame({ title, leftAction, details, children }: MinerViewFrameProps) {
  const [detailsOpen, setDetailsOpen] = useState(false);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          {leftAction ? (
            <Button
              text={leftAction.label}
              prefixIcon={<ArrowLeftCompact />}
              onClick={leftAction.onClick}
              size={buttonSizes.compact}
              variant={buttonVariants.ghost}
            />
          ) : null}
          <span className="text-heading-100 text-text-primary-50">{title}</span>
        </div>
        {details ? (
          <Button
            text="Details"
            prefixIcon={<Info />}
            onClick={() => setDetailsOpen(true)}
            size={buttonSizes.compact}
            variant={buttonVariants.secondary}
            ariaHasPopup="dialog"
            ariaExpanded={detailsOpen}
          />
        ) : null}
      </div>

      {details ? (
        <Modal
          open={detailsOpen}
          onDismiss={() => setDetailsOpen(false)}
          title="Miner details"
          size={modalSizes.large}
          divider
          buttons={[{ text: "Done", variant: buttonVariants.secondary, dismissModalOnClick: true }]}
        >
          {details}
        </Modal>
      ) : null}

      {/* The single-miner view sits on the app surface, set off from the grey
          prototype chrome above so it reads as an app preview. */}
      <div className="rounded-xl bg-surface-base p-6 shadow-100">{children}</div>
    </div>
  );
}
