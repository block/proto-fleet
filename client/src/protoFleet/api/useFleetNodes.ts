import { useCallback, useMemo } from "react";

import { fleetNodeAdminClient } from "@/protoFleet/api/clients";
import { type FleetNodeSummary } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListFleetNodesProps {
  onSuccess?: (nodes: FleetNodeSummary[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateEnrollmentCodeProps {
  onSuccess?: (code: string, expiresAt?: Date) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ConfirmFleetNodeProps {
  fleetNodeId: bigint;
  onSuccess?: (apiKey: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RevokeFleetNodeProps {
  fleetNodeId: bigint;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useFleetNodes = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listFleetNodes = useCallback(
    async ({ onSuccess, onError, onFinally }: ListFleetNodesProps) => {
      try {
        const response = await fleetNodeAdminClient.listFleetNodes({});
        onSuccess?.(response.fleetNodes);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createEnrollmentCode = useCallback(
    async ({ onSuccess, onError, onFinally }: CreateEnrollmentCodeProps) => {
      try {
        const response = await fleetNodeAdminClient.createEnrollmentCode({});
        const expiresAt = response.expiresAt ? new Date(Number(response.expiresAt.seconds) * 1000) : undefined;
        onSuccess?.(response.code, expiresAt);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const confirmFleetNode = useCallback(
    async ({ fleetNodeId, onSuccess, onError, onFinally }: ConfirmFleetNodeProps) => {
      try {
        const response = await fleetNodeAdminClient.confirmFleetNode({ fleetNodeId });
        onSuccess?.(response.apiKey);
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const revokeFleetNode = useCallback(
    async ({ fleetNodeId, onSuccess, onError, onFinally }: RevokeFleetNodeProps) => {
      try {
        await fleetNodeAdminClient.revokeFleetNode({ fleetNodeId });
        onSuccess?.();
      } catch (err) {
        handleAuthErrors({ error: err, onError: () => onError?.(getErrorMessage(err)) });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return useMemo(
    () => ({ listFleetNodes, createEnrollmentCode, confirmFleetNode, revokeFleetNode }),
    [listFleetNodes, createEnrollmentCode, confirmFleetNode, revokeFleetNode],
  );
};

export { useFleetNodes };
