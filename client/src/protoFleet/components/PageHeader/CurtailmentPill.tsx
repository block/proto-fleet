import { useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";
import Popover, { PopoverProvider, popoverSizes, useResponsivePopover } from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

export const curtailmentPillStates = ["pending", "active", "restoring"] as const;

export type CurtailmentPillState = (typeof curtailmentPillStates)[number];

export interface CurtailmentPillEvent {
  id: string;
  reason: string;
  state: CurtailmentPillState;
  scopeLabel: string;
  selectedMiners: number;
  estimatedReductionKw: number;
}

export interface CurtailmentPillProps {
  event: CurtailmentPillEvent;
  detailsPath?: string;
}

const eventStateLabels: Record<CurtailmentPillState, string> = {
  pending: "Pending",
  active: "Active",
  restoring: "Restoring",
};

const eventStateDotClassNames: Record<CurtailmentPillState, string> = {
  pending: "bg-core-accent-fill",
  active: "bg-intent-warning-fill",
  restoring: "bg-core-accent-fill",
};

function formatKw(value: number): string {
  return `${value.toLocaleString(undefined, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  })} kW`;
}

function formatMinerCount(minerCount: number): string {
  return `${minerCount.toLocaleString()} selected ${minerCount === 1 ? "miner" : "miners"}`;
}

function CurtailmentPillContent({ event, detailsPath }: CurtailmentPillProps) {
  const [isPopoverOpen, setIsPopoverOpen] = useState(false);
  const { triggerRef } = useResponsivePopover();
  const stateLabel = eventStateLabels[event.state];

  return (
    <div className="curtailment-pill-trigger relative" ref={triggerRef}>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        ariaHasPopup={true}
        ariaExpanded={isPopoverOpen}
        ariaLabel={`View curtailment details for ${event.reason}`}
        onClick={(clickEvent) => {
          setIsPopoverOpen((current) => !current);

          if (clickEvent.detail > 0) {
            clickEvent.currentTarget.blur();
          }
        }}
        prefixIcon={<span className={clsx("h-2.5 w-2.5 rounded-full", eventStateDotClassNames[event.state])} />}
      >
        <span className="block max-w-56 truncate">Curtailment {stateLabel.toLowerCase()}</span>
      </Button>

      {isPopoverOpen ? (
        <Popover
          position={positions["bottom left"]}
          size={popoverSizes.small}
          className="!space-y-0 px-4 pt-4 pb-3"
          closePopover={() => setIsPopoverOpen(false)}
          closeIgnoreSelectors={[".curtailment-pill-trigger"]}
        >
          <div className="flex flex-col gap-3">
            <div className="min-w-0 space-y-1">
              <div className="truncate text-heading-100 text-text-primary">{event.reason}</div>
              <div className="text-200 leading-snug text-text-primary-70">{stateLabel}</div>
              <div className="text-200 leading-snug text-text-primary-70">{event.scopeLabel}</div>
              <div className="text-200 leading-snug text-text-primary-70">
                {formatMinerCount(event.selectedMiners)} - {formatKw(event.estimatedReductionKw)} planned
              </div>
            </div>

            {detailsPath ? (
              <div className="border-t border-border-5 pt-3">
                <Link
                  to={detailsPath}
                  onClick={() => setIsPopoverOpen(false)}
                  className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
                >
                  View curtailment
                </Link>
              </div>
            ) : null}
          </div>
        </Popover>
      ) : null}
    </div>
  );
}

function CurtailmentPill(props: CurtailmentPillProps) {
  return (
    <PopoverProvider>
      <CurtailmentPillContent {...props} />
    </PopoverProvider>
  );
}

export default CurtailmentPill;
