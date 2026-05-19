import { useCallback } from "react";

import { sitesClient } from "@/protoFleet/api/clients";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListSitesProps {
  onSuccess?: (sites: SiteWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useSites = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listSites = useCallback(
    async ({ onSuccess, onError, onFinally }: ListSitesProps = {}) => {
      try {
        const response = await sitesClient.listSites({});
        onSuccess?.(response.sites);
      } catch (err) {
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
