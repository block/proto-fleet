import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  type CreateMqttCurtailmentSourceRequest,
  CreateMqttCurtailmentSourceRequestSchema,
  DeleteMqttCurtailmentSourceRequestSchema,
  ListMqttCurtailmentSourcesRequestSchema,
  type MqttCurtailmentSource,
  MqttCurtailmentSourceRuntimeState,
  SetMqttCurtailmentSourceEnabledRequestSchema,
  type TestMqttCurtailmentSourceConnectionRequest,
  TestMqttCurtailmentSourceConnectionRequestSchema,
  type UpdateMqttCurtailmentSourceRequest,
  UpdateMqttCurtailmentSourceRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import type {
  CurtailmentHealth,
  CurtailmentSource,
  CurtailmentSourceFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useAuthErrors } from "@/protoFleet/store";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const DEFAULT_BROKER_TRANSPORT = "tcp";
const DEFAULT_PAYLOAD_FORMAT = "target_timestamp";
const DEFAULT_STALENESS_THRESHOLD_SEC = 240;
const SOURCES_POLL_INTERVAL_MS = 10_000;

const unsetDisplayValue = "-";

export type ListSourcesOptions = {
  silent?: boolean;
};

export type UseMqttCurtailmentSourcesResult = {
  sources: CurtailmentSource[];
  isLoading: boolean;
  isCreating: boolean;
  isTestingConnection: boolean;
  updatingSourceIds: ReadonlySet<string>;
  loadError: string | null;
  createError: string | null;
  listSources: (signal?: AbortSignal, options?: ListSourcesOptions) => Promise<CurtailmentSource[]>;
  createSource: (values: CurtailmentSourceFormValues) => Promise<CurtailmentSource>;
  updateSource: (sourceId: string, values: CurtailmentSourceFormValues) => Promise<CurtailmentSource>;
  testConnection: (values: CurtailmentSourceFormValues) => Promise<void>;
  setSourceEnabled: (sourceId: string, enabled: boolean) => Promise<CurtailmentSource>;
  deleteSource: (sourceId: string) => Promise<void>;
};

function timestampToEpochSeconds(timestamp?: Timestamp): number | undefined {
  if (!timestamp) {
    return undefined;
  }

  const seconds = Number(timestamp.seconds);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}

function formatSignalUpdate(timestamp?: Timestamp): string {
  const seconds = timestampToEpochSeconds(timestamp);
  return seconds === undefined ? unsetDisplayValue : formatTimestamp(seconds, { includeSeconds: true });
}

function sourceHasReceivedSignal(source: MqttCurtailmentSource): boolean {
  return Boolean(source.status?.lastReceivedAt ?? source.status?.lastTargetAt);
}

function mapRuntimeHealth(source: MqttCurtailmentSource): CurtailmentHealth {
  if (!source.enabled) {
    return "offline";
  }

  const status = source.status;
  if (!status) {
    return "waitingForSignal";
  }

  const hasReceivedSignal = sourceHasReceivedSignal(source);
  if (!hasReceivedSignal && status.runtimeState !== MqttCurtailmentSourceRuntimeState.ERROR) {
    return "waitingForSignal";
  }

  if (status.stale) {
    return "noSignal";
  }

  switch (status.runtimeState) {
    case MqttCurtailmentSourceRuntimeState.UNSPECIFIED:
    case MqttCurtailmentSourceRuntimeState.STARTING:
      return "waitingForSignal";
    case MqttCurtailmentSourceRuntimeState.RUNNING:
      return "connected";
    default:
      return "offline";
  }
}

function mapMqttCurtailmentSource(source: MqttCurtailmentSource): CurtailmentSource {
  return {
    id: source.sourceId.toString(),
    name: source.sourceName,
    triggerType: "MQTT",
    brokerHosts: [source.brokerPrimaryHost, source.brokerSecondaryHost].filter(Boolean),
    brokerPrimaryHost: source.brokerPrimaryHost,
    brokerSecondaryHost: source.brokerSecondaryHost,
    port: source.brokerPort,
    topic: source.topic,
    protocol: source.brokerTransport ? source.brokerTransport.toUpperCase() : "MQTT",
    qos: 1,
    username: source.mqttUsername,
    hasPassword: source.hasPassword,
    lastTarget: source.status?.lastTarget || unsetDisplayValue,
    lastSeen: formatSignalUpdate(source.status?.lastReceivedAt ?? source.status?.lastTargetAt),
    health: mapRuntimeHealth(source),
    enabled: source.enabled,
  };
}

function buildCreateSourceRequest(values: CurtailmentSourceFormValues): CreateMqttCurtailmentSourceRequest {
  return create(CreateMqttCurtailmentSourceRequestSchema, {
    sourceName: values.name.trim(),
    topic: values.topic.trim(),
    brokerPrimaryHost: values.brokerPrimaryHost.trim(),
    brokerSecondaryHost: values.brokerSecondaryHost.trim(),
    brokerPort: Number(values.brokerPort),
    brokerTransport: DEFAULT_BROKER_TRANSPORT,
    mqttUsername: values.username.trim(),
    mqttPassword: values.password,
    payloadFormat: DEFAULT_PAYLOAD_FORMAT,
    stalenessThresholdSec: DEFAULT_STALENESS_THRESHOLD_SEC,
  });
}

function buildTestConnectionRequest(values: CurtailmentSourceFormValues): TestMqttCurtailmentSourceConnectionRequest {
  return create(TestMqttCurtailmentSourceConnectionRequestSchema, {
    topic: values.topic.trim(),
    brokerPrimaryHost: values.brokerPrimaryHost.trim(),
    brokerSecondaryHost: values.brokerSecondaryHost.trim(),
    brokerPort: Number(values.brokerPort),
    brokerTransport: DEFAULT_BROKER_TRANSPORT,
    mqttUsername: values.username.trim(),
    mqttPassword: values.password,
    payloadFormat: DEFAULT_PAYLOAD_FORMAT,
  });
}

function buildUpdateSourceRequest(
  sourceId: string,
  values: CurtailmentSourceFormValues,
): UpdateMqttCurtailmentSourceRequest {
  return create(UpdateMqttCurtailmentSourceRequestSchema, {
    sourceId: BigInt(sourceId),
    sourceName: values.name.trim(),
    topic: values.topic.trim(),
    brokerPrimaryHost: values.brokerPrimaryHost.trim(),
    brokerSecondaryHost: values.brokerSecondaryHost.trim(),
    brokerPort: Number(values.brokerPort),
    mqttUsername: values.username.trim(),
    ...(values.password === "" ? {} : { mqttPassword: values.password }),
  });
}

export default function useMqttCurtailmentSources(enabled = true): UseMqttCurtailmentSourcesResult {
  const { handleAuthErrors } = useAuthErrors();
  const [sources, setSources] = useState<CurtailmentSource[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isCreating, setIsCreating] = useState(false);
  const [isTestingConnection, setIsTestingConnection] = useState(false);
  const [updatingSourceIds, setUpdatingSourceIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const hasLoadedSourcesRef = useRef(false);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const listSources = useCallback(
    async (signal?: AbortSignal, options: ListSourcesOptions = {}): Promise<CurtailmentSource[]> => {
      const shouldShowLoading = !options.silent && !hasLoadedSourcesRef.current;
      if (shouldShowLoading) {
        setIsLoading(true);
      }

      try {
        assertNotAborted(signal);
        const response = await curtailmentClient.listMqttCurtailmentSources(
          create(ListMqttCurtailmentSourcesRequestSchema, {}),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        const nextSources = response.sources.map(mapMqttCurtailmentSource);
        setSources(nextSources);
        hasLoadedSourcesRef.current = true;
        setLoadError(null);
        return nextSources;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load curtailment sources.");
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

    let isActive = true;
    let pollTimerId: ReturnType<typeof setTimeout> | undefined;
    let abortController: AbortController | undefined;

    function schedulePoll(): void {
      if (!isActive) {
        return;
      }

      pollTimerId = setTimeout(() => {
        void refreshSources(true);
      }, SOURCES_POLL_INTERVAL_MS);
    }

    async function refreshSources(silent: boolean): Promise<void> {
      abortController?.abort();
      abortController = new AbortController();

      try {
        await listSources(abortController.signal, { silent });
      } catch {
        // The page already exposes load errors; polling should continue retrying in the background.
      } finally {
        schedulePoll();
      }
    }

    void refreshSources(false);

    return () => {
      isActive = false;
      if (pollTimerId) {
        clearTimeout(pollTimerId);
      }
      abortController?.abort();
    };
  }, [enabled, listSources]);

  const createSource = useCallback(
    async (values: CurtailmentSourceFormValues): Promise<CurtailmentSource> => {
      setIsCreating(true);
      setCreateError(null);

      try {
        const response = await curtailmentClient.createMqttCurtailmentSource(buildCreateSourceRequest(values));
        if (!response.source) {
          throw new Error("Created curtailment source response was missing a source.");
        }

        const nextSource = mapMqttCurtailmentSource(response.source);
        setSources((currentSources) => [
          ...currentSources.filter((currentSource) => currentSource.id !== nextSource.id),
          nextSource,
        ]);
        return nextSource;
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to create curtailment source.");
        setCreateError(resolvedError.message);
        throw resolvedError;
      } finally {
        setIsCreating(false);
      }
    },
    [handleFailure],
  );

  const testConnection = useCallback(
    async (values: CurtailmentSourceFormValues): Promise<void> => {
      setIsTestingConnection(true);

      try {
        const response = await curtailmentClient.testMqttCurtailmentSourceConnection(
          buildTestConnectionRequest(values),
        );
        if (!response.ok) {
          throw new Error("Failed to connect to the MaestroOS MQTT broker.");
        }
      } catch (error) {
        throw handleFailure(error, "Failed to connect to the MaestroOS MQTT broker.");
      } finally {
        setIsTestingConnection(false);
      }
    },
    [handleFailure],
  );

  const setSourceEnabled = useCallback(
    async (sourceId: string, enabled: boolean): Promise<CurtailmentSource> => {
      setUpdatingSourceIds((currentIds) => new Set(currentIds).add(sourceId));

      try {
        const response = await curtailmentClient.setMqttCurtailmentSourceEnabled(
          create(SetMqttCurtailmentSourceEnabledRequestSchema, {
            sourceId: BigInt(sourceId),
            enabled,
          }),
        );
        if (!response.source) {
          throw new Error("Updated curtailment source response was missing a source.");
        }

        const nextSource = mapMqttCurtailmentSource(response.source);
        setSources((currentSources) =>
          currentSources.map((currentSource) => (currentSource.id === nextSource.id ? nextSource : currentSource)),
        );
        return nextSource;
      } catch (error) {
        throw handleFailure(error, "Failed to update curtailment source.");
      } finally {
        setUpdatingSourceIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(sourceId);
          return nextIds;
        });
      }
    },
    [handleFailure],
  );

  const updateSource = useCallback(
    async (sourceId: string, values: CurtailmentSourceFormValues): Promise<CurtailmentSource> => {
      setUpdatingSourceIds((currentIds) => new Set(currentIds).add(sourceId));

      try {
        const response = await curtailmentClient.updateMqttCurtailmentSource(
          buildUpdateSourceRequest(sourceId, values),
        );
        if (!response.source) {
          throw new Error("Updated curtailment source response was missing a source.");
        }

        const nextSource = mapMqttCurtailmentSource(response.source);
        setSources((currentSources) =>
          currentSources.map((currentSource) => (currentSource.id === nextSource.id ? nextSource : currentSource)),
        );
        return nextSource;
      } catch (error) {
        throw handleFailure(error, "Failed to update curtailment source.");
      } finally {
        setUpdatingSourceIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(sourceId);
          return nextIds;
        });
      }
    },
    [handleFailure],
  );

  const deleteSource = useCallback(
    async (sourceId: string): Promise<void> => {
      setUpdatingSourceIds((currentIds) => new Set(currentIds).add(sourceId));

      try {
        await curtailmentClient.deleteMqttCurtailmentSource(
          create(DeleteMqttCurtailmentSourceRequestSchema, {
            sourceId: BigInt(sourceId),
          }),
        );
        setSources((currentSources) => currentSources.filter((currentSource) => currentSource.id !== sourceId));
      } catch (error) {
        throw handleFailure(error, "Failed to delete curtailment source.");
      } finally {
        setUpdatingSourceIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(sourceId);
          return nextIds;
        });
      }
    },
    [handleFailure],
  );

  return useMemo(
    () => ({
      sources,
      isLoading,
      isCreating,
      isTestingConnection,
      updatingSourceIds,
      loadError,
      createError,
      listSources,
      createSource,
      updateSource,
      testConnection,
      setSourceEnabled,
      deleteSource,
    }),
    [
      sources,
      isLoading,
      isCreating,
      isTestingConnection,
      updatingSourceIds,
      loadError,
      createError,
      listSources,
      createSource,
      updateSource,
      testConnection,
      setSourceEnabled,
      deleteSource,
    ],
  );
}
