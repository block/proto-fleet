import { useCallback, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import type { FleetNodeUpgradePillData } from "./PageHeader";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useHasPermission } from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

const NODE_SETTINGS_PATH = "/settings/nodes";

interface UseFleetNodeUpgradeIndicatorOptions {
  enabled?: boolean;
}

export function useFleetNodeUpgradeIndicator({
  enabled = true,
}: UseFleetNodeUpgradeIndicatorOptions = {}): FleetNodeUpgradePillData | null {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const canReadNodes = useHasPermission("fleetnode:read");
  const { listFleetNodes } = useFleetNodes();
  const [result, setResult] = useState<{ nodeCount: number; pollEpoch: symbol } | null>(null);
  const latestRequestId = useRef(0);
  const isNodeSettingsPath = pathname.replace(/\/+$/, "").toLowerCase() === NODE_SETTINGS_PATH;
  const pollEnabled = enabled && canReadNodes && !isNodeSettingsPath;
  const pollEpoch = useMemo(() => Symbol(pollEnabled ? "enabled" : "disabled"), [pollEnabled]);

  const refresh = useCallback(async () => {
    const requestId = ++latestRequestId.current;
    try {
      const nodes = await listFleetNodes();
      if (requestId === latestRequestId.current) {
        setResult({
          nodeCount: nodes.filter((node) => node.commandProtocolUpgradeRequired).length,
          pollEpoch,
        });
      }
    } catch {
      // Keep the shell quiet when a background refresh fails. The Nodes page
      // remains the authoritative place for load errors.
    }
  }, [listFleetNodes, pollEpoch]);

  usePoll({
    fetchData: refresh,
    params: refresh,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: pollEnabled,
  });

  const openNodeSettings = useCallback(() => {
    void navigate(NODE_SETTINGS_PATH);
  }, [navigate]);

  return useMemo(
    () =>
      pollEnabled && result?.pollEpoch === pollEpoch && result.nodeCount > 0
        ? { nodeCount: result.nodeCount, onClick: openNodeSettings }
        : null,
    [openNodeSettings, pollEnabled, pollEpoch, result],
  );
}
