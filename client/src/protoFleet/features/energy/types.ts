import {
  activeCurtailmentEventStates,
  type ActiveCurtailmentMappedEventState,
} from "@/protoFleet/api/curtailmentEventMappers";
import type {
  CurtailmentEvent,
  StartCurtailmentRequest,
  UpdateCurtailmentEventRequest,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type {
  ActiveCurtailmentEvent as ActiveCurtailmentStatusEvent,
  CurtailmentTargetRollup,
  CurtailmentTargetState,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  curtailmentEventStates,
  type CurtailmentEventState as DisplayCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { CurtailmentHistoryEvent as ComponentCurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import type { CurtailmentPriority } from "@/protoFleet/features/energy/CurtailmentStartModal";

export type CurtailmentMode = "fixedKw";

export { activeCurtailmentEventStates, curtailmentEventStates };
export type { CurtailmentPriority, CurtailmentTargetRollup, CurtailmentTargetState };

export type CurtailmentEventState = DisplayCurtailmentEventState;
export type ActiveCurtailmentEventState = ActiveCurtailmentMappedEventState;

export interface CurtailmentHistoryEvent extends ComponentCurtailmentHistoryEvent {
  state: CurtailmentEventState;
  mode: CurtailmentMode;
  modeLabel: string;
  priority: CurtailmentPriority;
  toleranceKw?: number;
  startedAt?: string;
  scheduledAt?: string;
  createdAt?: string;
  rawEvent?: CurtailmentEvent;
}

export interface CurtailmentActiveEvent extends ActiveCurtailmentStatusEvent {
  id: string;
  state: CurtailmentEventState;
  priority: CurtailmentPriority;
  sourceLabel: string;
  startedAt: string;
  modeLabel: string;
  targetSummary: string;
  toleranceKw?: number;
  rawEvent?: CurtailmentEvent;
}

export interface CurtailmentListState {
  activeEvent?: CurtailmentActiveEvent;
  events: CurtailmentHistoryEvent[];
}

export interface CurtailmentStartResult {
  event: CurtailmentActiveEvent;
}

export interface CurtailmentStopResult {
  event: CurtailmentActiveEvent;
}

export interface CurtailmentUpdateResult {
  event: CurtailmentActiveEvent;
}

export interface CurtailmentApi {
  activeEvent?: CurtailmentActiveEvent;
  events: CurtailmentHistoryEvent[];
  isLoading: boolean;
  refreshCurtailment: (stateFilters?: CurtailmentEventState[]) => Promise<CurtailmentListState>;
  startCurtailment: (request: StartCurtailmentRequest) => Promise<CurtailmentStartResult>;
  stopCurtailment: (eventId: string) => Promise<CurtailmentStopResult>;
  updateCurtailmentEvent: (request: UpdateCurtailmentEventRequest) => Promise<CurtailmentUpdateResult>;
}
