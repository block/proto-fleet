import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import type { CurtailmentPillEvent, CurtailmentPillProps, CurtailmentPillState } from "./curtailmentPillTypes";
import PageHeaderPopoverPill from "./PageHeaderPopoverPill";
import {
  activeCurtailmentDisplayStateConfigs,
  type CurtailmentEventState,
  curtailmentEventStateConfigs,
  formatCurtailmentKw,
  formatCurtailmentSelectedMinerCount,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";

export type { CurtailmentPillEvent, CurtailmentPillProps, CurtailmentPillState } from "./curtailmentPillTypes";

const unavailableTargetMetricsLabel = "Target details unavailable";

function getLegacyCurtailmentPillHeaderState(state: CurtailmentPillState): CurtailmentEventState {
  switch (state) {
    case "curtailing":
    case "curtailed":
      return "active";
    case "pending":
    case "restoring":
      return state;
  }
}

function getPlannedReductionDetail(event: CurtailmentPillEvent): string {
  if (!event.targetMetricsAvailable) {
    return unavailableTargetMetricsLabel;
  }

  const minerCountLabel = formatCurtailmentSelectedMinerCount(event.selectedMiners);
  // Summary-only rows carry a live count but no kW estimate; showing
  // "0.0 kW planned" would fabricate a zero estimate.
  if (event.estimatedReductionAvailable === false) {
    return minerCountLabel;
  }

  return `${minerCountLabel} - ${formatCurtailmentKw(event.estimatedReductionKw)} planned`;
}

function CurtailmentPill({ event, detailsPath }: CurtailmentPillProps): ReactElement {
  const stateConfig = activeCurtailmentDisplayStateConfigs[event.state];
  const headerStateConfig = curtailmentEventStateConfigs[getLegacyCurtailmentPillHeaderState(event.state)];
  const plannedReductionDetail = getPlannedReductionDetail(event);
  const detailRows = [
    { id: "state", value: stateConfig.label },
    { id: "scope", value: event.scopeLabel },
    { id: "planned-reduction", value: plannedReductionDetail },
  ];

  return (
    <PageHeaderPopoverPill
      ariaLabel={`View curtailment details for ${event.reason}`}
      dotClassName={headerStateConfig.dotClassName}
      triggerClassName="curtailment-pill-trigger"
      triggerLabel={`Curtailment ${headerStateConfig.label.toLowerCase()}`}
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
