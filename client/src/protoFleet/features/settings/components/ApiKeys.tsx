import { useCallback, useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";
import { useApiKeys } from "@/protoFleet/api/useApiKeys";
import type { ApiKeyItem } from "@/protoFleet/api/useApiKeys";
import CreateApiKeyModal from "@/protoFleet/features/settings/components/CreateApiKeyModal";
import RevokeApiKeyDialog from "@/protoFleet/features/settings/components/RevokeApiKeyDialog";
import { useRole } from "@/protoFleet/store";
import { Trash } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { ColConfig, ColTitles } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

type ApiKeyColumns = "name" | "prefix" | "createdAt" | "expiresAt" | "lastUsedAt" | "createdBy";

const colTitles: ColTitles<ApiKeyColumns> = {
  name: "Name",
  prefix: "Key",
  createdAt: "Created",
  expiresAt: "Expires",
  lastUsedAt: "Last Used",
  createdBy: "Created By",
};

const activeCols: ApiKeyColumns[] = ["name", "prefix", "createdAt", "expiresAt", "lastUsedAt", "createdBy"];

const ApiKeys = () => {
  const { listApiKeys, revokeApiKey } = useApiKeys();
  const currentUserRole = useRole();
  const [apiKeys, setApiKeys] = useState<ApiKeyItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [revokeKeyData, setRevokeKeyData] = useState<ApiKeyItem | null>(null);
  const [isRevoking, setIsRevoking] = useState(false);

  const isAdmin = currentUserRole === "SUPER_ADMIN" || currentUserRole === "ADMIN";

  const fetchApiKeys = useCallback(() => {
    setIsLoading(true);
    listApiKeys({
      onSuccess: (keys) => {
        setApiKeys(keys);
      },
      onError: (error) => {
        pushToast({
          message: error || "Failed to load API keys",
          status: STATUSES.error,
        });
      },
      onFinally: () => {
        setIsLoading(false);
      },
    });
  }, [listApiKeys]);

  useEffect(() => {
    if (isAdmin) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch when admin role resolves; setState inside async fetch is the external-sync pattern
      fetchApiKeys();
    }
  }, [fetchApiKeys, isAdmin]);

  const handleCreateSuccess = useCallback(() => {
    fetchApiKeys();
    setShowCreateModal(false);
  }, [fetchApiKeys]);

  const handleRevokeConfirm = useCallback(() => {
    if (!revokeKeyData) return;

    setIsRevoking(true);
    revokeApiKey({
      keyId: revokeKeyData.keyId,
      onSuccess: () => {
        pushToast({
          message: `API key "${revokeKeyData.name}" has been revoked`,
          status: STATUSES.success,
        });
        setRevokeKeyData(null);
        fetchApiKeys();
      },
      onError: (error) => {
        pushToast({
          message: error || "Failed to revoke API key",
          status: STATUSES.error,
        });
      },
      onFinally: () => {
        setIsRevoking(false);
      },
    });
  }, [revokeKeyData, revokeApiKey, fetchApiKeys]);

  const availableActions = useMemo(
    () => [
      {
        title: "Revoke",
        icon: <Trash />,
        variant: "destructive" as const,
        actionHandler: (key: ApiKeyItem) => setRevokeKeyData(key),
      },
    ],
    [],
  );

  const colConfig: ColConfig<ApiKeyItem, string, ApiKeyColumns> = useMemo(
    () => ({
      name: {
        component: (key: ApiKeyItem) => <span className="text-emphasis-300">{key.name}</span>,
        width: "w-48",
      },
      prefix: {
        component: (key: ApiKeyItem) => (
          <span className="font-mono text-200 text-text-primary-50">{key.prefix}...</span>
        ),
        width: "w-44",
      },
      createdAt: {
        component: (key: ApiKeyItem) => (
          <span>{key.createdAt ? formatTimestamp(Math.floor(key.createdAt.getTime() / 1000)) : "—"}</span>
        ),
        width: "w-40",
      },
      expiresAt: {
        component: (key: ApiKeyItem) => (
          <span>{key.expiresAt ? formatTimestamp(Math.floor(key.expiresAt.getTime() / 1000)) : "Never"}</span>
        ),
        width: "w-40",
      },
      lastUsedAt: {
        component: (key: ApiKeyItem) => (
          <span>{key.lastUsedAt ? formatTimestamp(Math.floor(key.lastUsedAt.getTime() / 1000)) : "Never"}</span>
        ),
        width: "w-40",
      },
      createdBy: {
        component: (key: ApiKeyItem) => <span>{key.createdBy}</span>,
        width: "w-36",
      },
    }),
    [],
  );

  // Redirect non-admins away — placed after all hooks to satisfy rules-of-hooks
  if (!isAdmin) {
    return <Navigate to="/settings/general" replace />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <Header title="API Keys" titleSize="text-heading-300" />
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Create API key"
          onClick={() => setShowCreateModal(true)}
        />
      </div>

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading API keys...</div>
      ) : (
        <List<ApiKeyItem, string, ApiKeyColumns>
          items={apiKeys}
          itemKey="keyId"
          activeCols={activeCols}
          colTitles={colTitles}
          colConfig={colConfig}
          total={apiKeys.length}
          itemName={{ singular: "key", plural: "keys" }}
          noDataElement={
            <div className="py-10 text-center text-text-primary-50">
              No API keys yet. Create your first key to enable programmatic access to the Fleet API.
            </div>
          }
          actions={availableActions}
        />
      )}

      <CreateApiKeyModal
        open={showCreateModal}
        onDismiss={() => setShowCreateModal(false)}
        onSuccess={handleCreateSuccess}
      />
      <RevokeApiKeyDialog
        open={!!revokeKeyData}
        keyName={revokeKeyData?.name ?? ""}
        onConfirm={handleRevokeConfirm}
        onDismiss={() => setRevokeKeyData(null)}
        isSubmitting={isRevoking}
      />
    </div>
  );
};

export default ApiKeys;
