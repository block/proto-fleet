import { useCallback } from "react";

import { sitesClient } from "@/protoFleet/api/clients";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListSitesProps {
  signal?: AbortSignal;
  onSuccess?: (sites: SiteWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

// Parse a string-encoded bigint id (the form we get from URL params and
// localStorage). Rejects empty strings, non-numeric input, and non-positive
// values so callers can short-circuit cleanly on bad input.
export const parseBigIntId = (value: unknown): bigint | null => {
  if (typeof value !== "string" || value.trim() === "") return null;
  try {
    const parsed = BigInt(value);
    return parsed > 0n ? parsed : null;
  } catch {
    return null;
  }
};

// Build the set of known site ids (decimal-string form) from a ListSites
// response. Centralised so SitePicker, SitesPage, SettingsSitesPage, and
// SitesAllTable can't drift on the derivation rule.
export const buildKnownSiteIds = (sites: SiteWithCounts[] | undefined): Set<string> => {
  if (!sites) return new Set();
  return new Set(sites.map((s) => (s.site?.id ?? 0n).toString()).filter((id) => id !== "0"));
};

const useSites = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listSites = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ListSitesProps = {}) => {
      try {
        const response = await sitesClient.listSites({}, { signal });
        if (signal?.aborted) return;
        onSuccess?.(response.sites);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return { listSites };
};

export { useSites };
