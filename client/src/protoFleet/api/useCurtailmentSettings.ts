import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  type CurtailmentSettings as ApiCurtailmentSettings,
  GetCurtailmentSettingsRequestSchema,
  UpdateCurtailmentSettingsRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import type { CurtailmentSettings } from "@/protoFleet/features/settings/components/Curtailment/types";
import { useAuthErrors } from "@/protoFleet/store";

export type UseCurtailmentSettingsResult = {
  settings: CurtailmentSettings | null;
  isLoading: boolean;
  isSaving: boolean;
  loadError: string | null;
  getSettings: (signal?: AbortSignal) => Promise<CurtailmentSettings>;
  updateSettings: (settings: CurtailmentSettings) => Promise<CurtailmentSettings>;
};

function mapApiSettings(settings?: ApiCurtailmentSettings): CurtailmentSettings {
  if (!settings) {
    throw new Error("Curtailment settings response was missing settings.");
  }

  return {
    postEventCooldownSec: settings.postEventCooldownSec,
  };
}

export default function useCurtailmentSettings(enabled = true): UseCurtailmentSettingsResult {
  const { handleAuthErrors } = useAuthErrors();
  const [settings, setSettings] = useState<CurtailmentSettings | null>(null);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isSaving, setIsSaving] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const hasLoadedRef = useRef(false);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const getSettings = useCallback(
    async (signal?: AbortSignal): Promise<CurtailmentSettings> => {
      const shouldShowLoading = !hasLoadedRef.current;
      if (shouldShowLoading) {
        setIsLoading(true);
      }

      try {
        assertNotAborted(signal);
        const response = await curtailmentClient.getCurtailmentSettings(
          create(GetCurtailmentSettingsRequestSchema, {}),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        const nextSettings = mapApiSettings(response.settings);
        setSettings(nextSettings);
        hasLoadedRef.current = true;
        setLoadError(null);
        return nextSettings;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load curtailment settings.");
        setLoadError(resolvedError.message);
        throw resolvedError;
      } finally {
        if (shouldShowLoading) {
          setIsLoading(false);
        }
      }
    },
    [handleFailure],
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const abortController = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    void getSettings(abortController.signal).catch(() => {});

    return () => {
      abortController.abort();
    };
  }, [enabled, getSettings]);

  const updateSettings = useCallback(
    async (nextSettings: CurtailmentSettings): Promise<CurtailmentSettings> => {
      setIsSaving(true);

      try {
        const response = await curtailmentClient.updateCurtailmentSettings(
          create(UpdateCurtailmentSettingsRequestSchema, {
            postEventCooldownSec: nextSettings.postEventCooldownSec,
          }),
        );
        const updatedSettings = mapApiSettings(response.settings);
        setSettings(updatedSettings);
        setLoadError(null);
        return updatedSettings;
      } catch (error) {
        throw handleFailure(error, "Failed to save curtailment settings.");
      } finally {
        setIsSaving(false);
      }
    },
    [handleFailure],
  );

  return useMemo(
    () => ({
      settings,
      isLoading: enabled ? isLoading : false,
      isSaving,
      loadError,
      getSettings,
      updateSettings,
    }),
    [enabled, getSettings, isLoading, isSaving, loadError, settings, updateSettings],
  );
}
