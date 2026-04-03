import { useCallback } from "react";

import { apiKeyClient } from "@/protoFleet/api/clients";
import type { ApiKeyInfo } from "@/protoFleet/api/generated/apikey/v1/apikey_pb";
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
  onSuccess?: (apiKey: string, info: ApiKeyItem) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
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
    createdAt: info.createdAt ? new Date(Number(info.createdAt.seconds) * 1000) : null,
    expiresAt: info.expiresAt ? new Date(Number(info.expiresAt.seconds) * 1000) : null,
    lastUsedAt: info.lastUsedAt ? new Date(Number(info.lastUsedAt.seconds) * 1000) : null,
    createdBy: info.createdBy,
  };
}

const useApiKeys = () => {
  const { handleAuthErrors } = useAuthErrors();

  const createApiKey = useCallback(
    async ({ name, expiresAt, onSuccess, onError, onFinally }: CreateApiKeyProps) => {
      await apiKeyClient
        .createApiKey({
          name,
          expiresAt: expiresAt ? { seconds: BigInt(Math.floor(expiresAt.getTime() / 1000)), nanos: 0 } : undefined,
        })
        .then((response) => {
          if (response.info) {
            onSuccess?.(response.apiKey, toApiKeyItem(response.info));
          } else {
            onError?.("Received an unexpected response from the server. Please try again.");
          }
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: () => {
              onError?.(err?.message ?? String(err));
            },
          });
        })
        .finally(() => {
          onFinally?.();
        });
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
              onError?.(err?.message ?? String(err));
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
              onError?.(err?.message ?? String(err));
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
