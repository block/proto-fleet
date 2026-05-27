import { Link } from "react-router-dom";

import type { CurtailmentPillProps } from "./curtailmentPillTypes";
import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import {
  curtailmentEventStateConfigs,
  formatCurtailmentKw,
  formatCurtailmentSelectedMinerCount,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export type { CurtailmentPillEvent, CurtailmentPillProps, CurtailmentPillState } from "./curtailmentPillTypes";

function CurtailmentPill({ event, detailsPath }: CurtailmentPillProps) {
  const stateConfig = curtailmentEventStateConfigs[event.state];
  const plannedReductionDetail = `${formatCurtailmentSelectedMinerCount(event.selectedMiners)} - ${formatCurtailmentKw(
    event.estimatedReductionKw,
  )} planned`;
  const detailRows = [
    { id: "state", value: stateConfig.label },
    { id: "scope", value: event.scopeLabel },
    { id: "planned-reduction", value: plannedReductionDetail },
  ];

  return (
    <PageHeaderPopoverPill
      ariaLabel={`View curtailment details for ${event.reason}`}
      dotClassName={stateConfig.dotClassName}
      triggerClassName="curtailment-pill-trigger"
      triggerLabel={`Curtailment ${stateConfig.label.toLowerCase()}`}
    >
      {({ closePopover }) => (
        <div className="flex flex-col gap-3">
          <div className="min-w-0 space-y-1">
            <div className="truncate text-heading-100 text-text-primary">{event.reason}</div>
            {detailRows.map(({ id, value }) => (
              <div key={id} className="text-200 leading-snug text-text-primary-70">
                {value}
              </div>
            ))}
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
