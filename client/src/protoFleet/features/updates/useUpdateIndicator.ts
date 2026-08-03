import { useCallback, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import type { UpdatePillData } from "@/protoFleet/components/PageHeader/PageHeader";
import { useAvailableUpdate } from "@/protoFleet/features/updates/api/useAvailableUpdate";

const UPDATE_SETTINGS_PATH = "/settings/updates";

interface UseUpdateIndicatorOptions {
  enabled?: boolean;
}

/**
 * Returns a passive Fleet-shell indicator. It deliberately carries only a
 * version and navigates to the authoritative Settings page; release notes and
 * installer commands never live in shell notification state.
 */
export function useUpdateIndicator({ enabled = true }: UseUpdateIndicatorOptions = {}): UpdatePillData | null {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const availableVersion = useAvailableUpdate({ enabled: enabled && pathname !== UPDATE_SETTINGS_PATH });

  const openUpdateSettings = useCallback(() => {
    void navigate(UPDATE_SETTINGS_PATH);
  }, [navigate]);

  return useMemo(
    () => (availableVersion ? { version: availableVersion, onClick: openUpdateSettings } : null),
    [availableVersion, openUpdateSettings],
  );
}
