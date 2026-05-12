export type CurtailmentMode = "fixedKw";
export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "deviceSet" | "explicitMiners";

export interface CurtailmentCandidate {
  deviceIdentifier: string;
  currentPowerW: number;
  efficiencyJth: number;
  reasonSelected: string;
}

export interface CurtailmentSkippedCandidate {
  deviceIdentifier: string;
  reason: string;
  currentPowerW?: number;
}

export interface CurtailmentPlanPreview {
  mode: CurtailmentMode;
  targetKw: number;
  toleranceKw?: number;
  estimatedReductionKw: number;
  estimatedRemainingPowerKw: number;
  preEventPowerKw: number;
  selectedCandidateCount: number;
  eligibleCandidateCount: number;
  selectedCandidates: CurtailmentCandidate[];
  skippedCandidates: CurtailmentSkippedCandidate[];
}

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
  deviceSetIds: string[];
  deviceIdentifiers: string[];
  targetKw: string;
  toleranceKw: string;
  priority: CurtailmentPriority;
  minCurtailedDurationSec: string;
  maxDurationSec: string;
  restoreBatchSize: string;
  restoreBatchIntervalSec: string;
  includeMaintenance: boolean;
  forceIncludeMaintenance: boolean;
  reason: string;
}
