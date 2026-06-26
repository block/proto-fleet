import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { buildSingleMinerRouteState, canOpenEmbeddedMinerView, rememberSingleMinerMetadata } from "./routeState";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

/**
 * Opens a miner the same way from every entry point (row click, actions menu):
 * the embedded single-miner view when the miner can be proxied, otherwise the
 * miner's own web UI in a new tab. Centralized so the callers can't drift —
 * the embed gate must match what the server proxy can actually serve.
 */
export const useOpenMinerView = () => {
  const navigate = useNavigate();

  return useCallback(
    (miner: MinerStateSnapshot) => {
      if (canOpenEmbeddedMinerView(miner)) {
        rememberSingleMinerMetadata(miner);
        navigate(`/miners/${encodeURIComponent(miner.deviceIdentifier)}`, {
          state: buildSingleMinerRouteState(miner),
        });
        return;
      }
      if (miner.url) {
        window.open(miner.url, "_blank", "noopener,noreferrer");
      }
    },
    [navigate],
  );
};
