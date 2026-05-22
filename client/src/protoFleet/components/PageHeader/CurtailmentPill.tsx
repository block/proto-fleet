import { Link } from "react-router-dom";
import clsx from "clsx";

import type { CurtailmentPillProps } from "./curtailmentPillTypes";
import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import {
  curtailmentEventStateDotClassNames,
  curtailmentEventStateLabels,
  formatCurtailmentKw,
  formatCurtailmentSelectedMinerCount,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export type { CurtailmentPillEvent, CurtailmentPillProps, CurtailmentPillState } from "./curtailmentPillTypes";

function CurtailmentPill({ event, detailsPath }: CurtailmentPillProps) {
  const stateLabel = curtailmentEventStateLabels[event.state];

  return (
    <PageHeaderPopoverPill
      ariaLabel={`View curtailment details for ${event.reason}`}
      prefixIcon={
        <span className={clsx("h-2.5 w-2.5 rounded-full", curtailmentEventStateDotClassNames[event.state])} />
      }
      triggerClassName="curtailment-pill-trigger"
      triggerContent={<span className="block max-w-56 truncate">Curtailment {stateLabel.toLowerCase()}</span>}
    >
      {({ closePopover }) => (
        <div className="flex flex-col gap-3">
          <div className="min-w-0 space-y-1">
            <div className="truncate text-heading-100 text-text-primary">{event.reason}</div>
            <div className="text-200 leading-snug text-text-primary-70">{stateLabel}</div>
            <div className="text-200 leading-snug text-text-primary-70">{event.scopeLabel}</div>
            <div className="text-200 leading-snug text-text-primary-70">
              {formatCurtailmentSelectedMinerCount(event.selectedMiners)} -{" "}
              {formatCurtailmentKw(event.estimatedReductionKw)} planned
            </div>
          </div>

          {detailsPath ? (
            <div className="border-t border-border-5 pt-3">
              <Link
                to={detailsPath}
                onClick={closePopover}
                className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
              >
                View curtailment
              </Link>
            </div>
          ) : null}
        </div>
      )}
    </PageHeaderPopoverPill>
  );
}

export default CurtailmentPill;
