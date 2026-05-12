import type { CurtailmentFormValues, CurtailmentPlanPreview } from "@/protoFleet/features/energy/types";

export const defaultCurtailmentFormValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  minCurtailedDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "",
  restoreBatchIntervalSec: "",
  includeMaintenance: false,
  forceIncludeMaintenance: false,
  reason: "",
};

export const storybookCurtailmentFormValues: CurtailmentFormValues = {
  ...defaultCurtailmentFormValues,
  targetKw: "60",
  minCurtailedDurationSec: "300",
  maxDurationSec: "3600",
  restoreBatchSize: "10",
  restoreBatchIntervalSec: "120",
  reason: "Grid peak - ERCOT 4CP signal",
};

export const mockPreview: CurtailmentPlanPreview = {
  mode: "fixedKw",
  targetKw: 60,
  estimatedReductionKw: 60.2,
  estimatedRemainingPowerKw: 131.7,
  preEventPowerKw: 191.9,
  selectedCandidateCount: 18,
  eligibleCandidateCount: 57,
  selectedCandidates: [
    { deviceIdentifier: "rig-b12-012", currentPowerW: 3351, efficiencyJth: 21.5, reasonSelected: "High J/TH" },
    { deviceIdentifier: "rig-b13-004", currentPowerW: 3383, efficiencyJth: 21.5, reasonSelected: "High J/TH" },
    { deviceIdentifier: "rig-c04-007", currentPowerW: 3380, efficiencyJth: 21.5, reasonSelected: "High J/TH" },
    { deviceIdentifier: "rig-c04-004", currentPowerW: 3389, efficiencyJth: 21.2, reasonSelected: "High J/TH" },
    { deviceIdentifier: "rig-b12-018", currentPowerW: 3294, efficiencyJth: 20.8, reasonSelected: "Underperformer" },
  ],
  skippedCandidates: [
    { deviceIdentifier: "rig-b12-007", reason: "unreachable_residual_load", currentPowerW: 340 },
    { deviceIdentifier: "rig-b12-013", reason: "updating", currentPowerW: 3476 },
    { deviceIdentifier: "rig-c04-013", reason: "unreachable_residual_load" },
  ],
};
