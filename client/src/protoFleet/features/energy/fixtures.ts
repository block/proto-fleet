import { curtailingCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus.fixtures";
import type { CurtailmentHistoryEvent as ComponentCurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import { mockCurtailmentHistoryEvents } from "@/protoFleet/features/energy/CurtailmentHistory.fixtures";
import type { CurtailmentActiveEvent, CurtailmentHistoryEvent } from "@/protoFleet/features/energy/types";

function getModeLabel(event: ComponentCurtailmentHistoryEvent): string {
  return `${(event.targetKw ?? event.estimatedReductionKw).toLocaleString()} kW target`;
}

function mapHistoryEvent(event: ComponentCurtailmentHistoryEvent): CurtailmentHistoryEvent {
  return {
    ...event,
    mode: "fixedKw",
    modeLabel: getModeLabel(event),
    priority: event.priority === "emergency" ? "emergency" : "normal",
    startedAt: event.startedAt ?? "2026-04-30T13:58:00-04:00",
  };
}

export const mockHistoryEvents: CurtailmentHistoryEvent[] = mockCurtailmentHistoryEvents.map(mapHistoryEvent);

const activeHistoryEvent = mockHistoryEvents[0];

if (activeHistoryEvent === undefined) {
  throw new Error("Expected a mock active curtailment history event.");
}

export const mockActiveEvent: CurtailmentActiveEvent = {
  ...curtailingCurtailmentEvent,
  id: activeHistoryEvent.id,
  priority: activeHistoryEvent.priority,
  sourceLabel: activeHistoryEvent.sourceLabel,
  startedAt: activeHistoryEvent.startedAt ?? "2026-04-30T13:58:00-04:00",
  modeLabel: activeHistoryEvent.modeLabel,
  targetSummary: `${curtailingCurtailmentEvent.selectedMiners} of 57 candidates`,
};
