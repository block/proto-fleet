export type CurtailmentPriority = "normal" | "emergency";
export type CurtailmentScopeType = "wholeOrg" | "deviceSet" | "explicitMiners";
export type ResponseProfileId = "customPlan";
export type CurtailmentMode = "fixedKwReduction";
export type MinerSelectionStrategy = "leastEfficientFirst";

export interface CurtailmentFormValues {
  scopeType: CurtailmentScopeType;
  scopeId?: string;
  deviceSetIds: string[];
  deviceIdentifiers: string[];
  responseProfileId: ResponseProfileId;
  curtailmentMode: CurtailmentMode;
  minerSelectionStrategy: MinerSelectionStrategy;
  targetKw: string;
  toleranceKw: string;
  priority: CurtailmentPriority;
  minDurationSec: string;
  maxDurationSec: string;
  restoreBatchSize: string;
  restoreIntervalSec: string;
  reason: string;
  includeMaintenance: boolean;
}

export type CurtailmentSubmitValues = CurtailmentFormValues;

export interface CurtailmentPlanPreview {
  selectedMinerCount: number;
  targetKw: number;
  estimatedReductionKw: number;
  curtailEstimate: string;
  restoreEstimate: string;
  scopeLabel: string;
}

export type CurtailmentFormErrors = Partial<Record<keyof CurtailmentFormValues, string>>;
