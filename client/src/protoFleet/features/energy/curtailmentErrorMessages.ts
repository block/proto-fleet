import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const insufficientLoadPattern =
  /^insufficient curtailable load: ([\d.]+) kW available, ([\d.]+) kW requested, tolerance ([\d.]+) kW, candidate_min_power_w=(\d+)W(?:, excluded: (.*))?$/;

const exclusionReasonLabels: Record<string, string> = {
  below_candidate_min_power_w: "below the minimum power floor",
  phantom_load_no_hash: "drawing power but not hashing",
  power_telemetry_unreliable: "missing reliable power telemetry",
  unreachable_residual_load: "offline",
  maintenance: "in maintenance",
  updating: "updating",
  reboot_required: "requiring reboot",
  stale_telemetry: "missing recent telemetry",
  non_actionable_status: "not actionable",
  pairing: "not paired",
  cooldown: "cooling down from recent curtailment",
  curtail_full_unsupported: "without curtailment support",
  active_event: "already in an active curtailment",
};

function formatExclusionSummary(excludedReasons: string | undefined): string | undefined {
  if (!excludedReasons) {
    return undefined;
  }

  const labels = excludedReasons
    .split(",")
    .map((entry) => {
      const [reason, count] = entry.trim().split("=");
      const label = exclusionReasonLabels[reason];
      return label && count ? `${count} ${label}` : undefined;
    })
    .filter((label): label is string => label !== undefined);

  return labels.length > 0 ? ` Excluded miners: ${labels.join(", ")}.` : undefined;
}

export function formatCurtailmentErrorMessage(message: string): string {
  const match = insufficientLoadPattern.exec(message);
  if (!match) {
    return message;
  }

  const [, availableKw, requestedKw, toleranceKw, candidateMinPowerW, excludedReasons] = match;
  const exclusionSummary = formatExclusionSummary(excludedReasons);

  if (Number(availableKw) === 0) {
    return `No miners are currently eligible in this scope. If you selected a site, make sure miners are assigned to that site and reporting recent power and hashrate telemetry. Miners must be paired, actionable, not already curtailed or cooling down, and drawing at least ${candidateMinPowerW} W.${exclusionSummary ?? ""}`;
  }

  return `Only ${availableKw} kW is currently eligible in this scope. The request needs ${requestedKw} kW with ${toleranceKw} kW tolerance, and miners below ${candidateMinPowerW} W are excluded.${exclusionSummary ?? ""} Select another site or miner scope, lower the target, or increase tolerance.`;
}

export function getCurtailmentErrorMessage(error: unknown, fallback: string): string {
  return formatCurtailmentErrorMessage(getErrorMessage(error, fallback));
}
