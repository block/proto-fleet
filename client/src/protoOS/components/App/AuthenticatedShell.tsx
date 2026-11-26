import { useEffect } from "react";

import { useErrors } from "@/protoOS/api/hooks/useErrors";
import { useFirmwareUpdate } from "@/protoOS/api/hooks/useFirmwareUpdate";
import { useNetworkInfo } from "@/protoOS/api/hooks/useNetworkInfo";
import { usePoolsInfo } from "@/protoOS/api/hooks/usePoolsInfo";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useMinerStore from "@/protoOS/store/useMinerStore";

interface AuthenticatedShellProps {
  reloadSystemInfo: () => void;
}

const AuthenticatedShell = ({ reloadSystemInfo }: AuthenticatedShellProps) => {
  const { adapter } = useMinerHosting();
  useEffect(() => {
    const now = adapter?.telemetryNow;
    if (!now) return;
    const controller = new AbortController();
    const { signal } = controller;
    const checkReadings = (resumed: boolean) => {
      const refresh = resumed ? adapter.resumeTelemetryClock?.({ signal }) : adapter.checkTelemetryClock?.({ signal });
      void refresh?.catch((error) => {
        if (!signal.aborted) console.error("Could not refresh the controller clock", error);
      });
      const currentTime = now();
      const { telemetry, minerStatus } = useMinerStore.getState();
      telemetry.refreshBoardTemperatures(currentTime);
      telemetry.refreshPsuReadings(currentTime);
      minerStatus.refreshThermalFaults(currentTime);
    };
    const resume = () => {
      if (!document.hidden) checkReadings(true);
    };
    document.addEventListener("visibilitychange", resume, { signal });
    window.addEventListener("focus", resume, { signal });
    window.addEventListener("pageshow", resume, { signal });
    const timer = setInterval(() => checkReadings(false), 1_000);
    return () => {
      clearInterval(timer);
      controller.abort();
    };
  }, [adapter]);

  useNetworkInfo({ poll: false });
  useErrors({ poll: true, pollIntervalMs: 15 * 1000 });
  usePoolsInfo({ poll: true, pollIntervalMs: 15 * 1000 });

  // Per-ASIC hashboard status is fetched lazily by the views that consume it
  // (HashboardTemperature for one board, Diagnostics for the board grid) rather
  // than eagerly here — a CT1 container is up to 864 boards, and no top-level
  // view renders ASIC-level data, so the shell must not fan out on every session.

  const { checkFirmwareUpdate } = useFirmwareUpdate();
  useEffect(() => {
    const checkForFirmwareUpdates = () => {
      checkFirmwareUpdate()
        .then(() => {
          reloadSystemInfo();
        })
        .catch((error) => {
          console.error("Error checking for firmware updates:", error);
        });
    };

    checkForFirmwareUpdates();
  }, [checkFirmwareUpdate, reloadSystemInfo]);

  return null;
};

export default AuthenticatedShell;
