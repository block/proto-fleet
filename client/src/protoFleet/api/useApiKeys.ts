import { useCallback } from "react";

import { apiKeyClient } from "@/protoFleet/api/clients";
import type { ApiKeyInfo } from "@/protoFleet/api/generated/apikey/v1/apikey_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

export interface ApiKeyItem {
  keyId: string;
  name: string;
  prefix: string;
  createdAt: Date | null;
  expiresAt: Date | null;
  lastUsedAt: Date | null;
  createdBy: string;
}

interface CreateApiKeyProps {
  name: string;
  expiresAt?: Date;
}

interface ListApiKeysProps {
  onSuccess?: (keys: ApiKeyItem[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface RevokeApiKeyProps {
  keyId: string;
  onSuccess?: () => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

function toApiKeyItem(info: ApiKeyInfo): ApiKeyItem {
  return {
    keyId: info.keyId,
    name: info.name,
    prefix: info.prefix,
    createdAt: info.createdAt && info.createdAt.seconds > 0 ? new Date(Number(info.createdAt.seconds) * 1000) : null,
    expiresAt: info.expiresAt && info.expiresAt.seconds > 0 ? new Date(Number(info.expiresAt.seconds) * 1000) : null,
    lastUsedAt:
      info.lastUsedAt && info.lastUsedAt.seconds > 0 ? new Date(Number(info.lastUsedAt.seconds) * 1000) : null,
    createdBy: info.createdBy,
  };
}

const useApiKeys = () => {
  const { handleAuthErrors } = useAuthErrors();

  const createApiKey = useCallback(
    async ({ name, expiresAt }: CreateApiKeyProps): Promise<string> => {
      try {
        const response = await apiKeyClient.createApiKey({
          name,
          expiresAt: expiresAt ? { seconds: BigInt(Math.floor(expiresAt.getTime() / 1000)), nanos: 0 } : undefined,
        });

        if (!response.info) {
          throw new Error("Received an unexpected response from the server. Please try again.");
        }

        return response.apiKey;
      } catch (err) {
        handleAuthErrors({ error: err });
        throw err instanceof Error ? err : new Error(String(err));
      }
    },
    [handleAuthErrors],
  );

  const listApiKeys = useCallback(
    async ({ onSuccess, onError, onFinally }: ListApiKeysProps) => {
      await apiKeyClient
        .listApiKeys({})
        .then((response) => {
          onSuccess?.(response.apiKeys.map(toApiKeyItem));
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  const revokeApiKey = useCallback(
    async ({ keyId, onSuccess, onError, onFinally }: RevokeApiKeyProps) => {
      await apiKeyClient
        .revokeApiKey({ keyId })
        .then(() => {
          onSuccess?.();
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(getErrorMessage(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
    },
    [handleAuthErrors],
  );

  return { createApiKey, listApiKeys, revokeApiKey };
};

export { useApiKeys };
