import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CreateMqttCurtailmentSourceRequestSchema,
  ListMqttCurtailmentSourcesRequestSchema,
  type MqttCurtailmentSource,
  MqttCurtailmentSourceRuntimeState,
  MqttCurtailmentSourceScopeSchema,
  MqttCurtailmentSourceScopeType,
  SetMqttCurtailmentSourceEnabledRequestSchema,
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
const DEFAULT_CURTAIL_MODE = "FULL_FLEET";
const DEFAULT_PAYLOAD_FORMAT = "target_timestamp";
const DEFAULT_STALENESS_THRESHOLD_SEC = 240;
const DEFAULT_MIN_CURTAILED_DURATION_SEC = 600;

const unsetDisplayValue = "-";

function timestampToEpochSeconds(timestamp?: Timestamp): number | undefined {
  if (!timestamp) {
    return undefined;
  }

  const seconds = Number(timestamp.seconds);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}

function formatSignalUpdate(timestamp?: Timestamp): string {
  const seconds = timestampToEpochSeconds(timestamp);
  return seconds === undefined ? unsetDisplayValue : formatTimestamp(seconds);
}

function formatScope(source: MqttCurtailmentSource): string {
  switch (source.scope?.type) {
    case MqttCurtailmentSourceScopeType.WHOLE_ORG:
      return "Whole organization";
    case MqttCurtailmentSourceScopeType.SITE:
      return source.scope.siteId ? `Site ${source.scope.siteId.toString()}` : "Site";
    case MqttCurtailmentSourceScopeType.DEVICE_LIST:
      return `${source.scope.deviceIdentifiers.length} devices`;
    default:
      return unsetDisplayValue;
  }
}

function mapRuntimeHealth(source: MqttCurtailmentSource): CurtailmentHealth {
  if (!source.enabled) {
    return "offline";
  }

  if (source.status?.stale) {
    return "noSignal";
  }

  switch (source.status?.runtimeState) {
    case MqttCurtailmentSourceRuntimeState.RUNNING:
      return "connected";
    case MqttCurtailmentSourceRuntimeState.STARTING:
      return "noSignal";
    default:
      return "offline";
  }
}

function mapMqttCurtailmentSource(source: MqttCurtailmentSource): CurtailmentSource {
  return {
    id: source.sourceId.toString(),
    name: source.sourceName,
    triggerType: "MQTT",
    site: formatScope(source),
    brokerHosts: [source.brokerPrimaryHost, source.brokerSecondaryHost].filter(Boolean),
    port: source.brokerPort,
    topic: source.topic,
    protocol: source.brokerTransport ? source.brokerTransport.toUpperCase() : "MQTT",
    qos: 1,
    username: source.mqttUsername,
    scope: formatScope(source),
    curtailmentMode: source.curtailMode || unsetDisplayValue,
    lastTarget: source.status?.lastTarget || unsetDisplayValue,
    lastSeen: formatSignalUpdate(source.status?.lastReceivedAt ?? source.status?.lastTargetAt),
    health: mapRuntimeHealth(source),
    enabled: source.enabled,
  };
}

function buildCreateSourceRequest(values: CurtailmentSourceFormValues) {
  return create(CreateMqttCurtailmentSourceRequestSchema, {
    sourceName: values.name.trim(),
    topic: values.topic.trim(),
    brokerPrimaryHost: values.brokerPrimaryHost.trim(),
    brokerSecondaryHost: values.brokerSecondaryHost.trim(),
    brokerPort: Number(values.brokerPort),
    brokerTransport: DEFAULT_BROKER_TRANSPORT,
    mqttUsername: values.username.trim(),
    mqttPassword: values.password,
    curtailMode: DEFAULT_CURTAIL_MODE,
    payloadFormat: DEFAULT_PAYLOAD_FORMAT,
    scope: create(MqttCurtailmentSourceScopeSchema, {
      type: MqttCurtailmentSourceScopeType.WHOLE_ORG,
    }),
    stalenessThresholdSec: DEFAULT_STALENESS_THRESHOLD_SEC,
    minCurtailedDurationSec: DEFAULT_MIN_CURTAILED_DURATION_SEC,
    enabled: true,
  });
}

export default function useMqttCurtailmentSources(enabled = true) {
  const { handleAuthErrors } = useAuthErrors();
  const [sources, setSources] = useState<CurtailmentSource[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isCreating, setIsCreating] = useState(false);
  const [updatingSourceIds, setUpdatingSourceIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string) => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const listSources = useCallback(
    async (signal?: AbortSignal) => {
      setIsLoading(true);

      try {
        assertNotAborted(signal);
        const response = await curtailmentClient.listMqttCurtailmentSources(
          create(ListMqttCurtailmentSourcesRequestSchema, {}),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        const nextSources = response.sources.map(mapMqttCurtailmentSource);
        setSources(nextSources);
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
        setIsLoading(false);
      }
    },
    [handleFailure],
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const abortController = new AbortController();
    queueMicrotask(() => {
      if (!abortController.signal.aborted) {
        void listSources(abortController.signal).catch(() => undefined);
      }
    });

    return () => abortController.abort();
  }, [enabled, listSources]);

  const createSource = useCallback(
    async (values: CurtailmentSourceFormValues) => {
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

  const setSourceEnabled = useCallback(
    async (sourceId: string, enabled: boolean) => {
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

  return useMemo(
    () => ({
      sources,
      isLoading,
      isCreating,
      updatingSourceIds,
      loadError,
      createError,
      listSources,
      createSource,
      setSourceEnabled,
    }),
    [
      sources,
      isLoading,
      isCreating,
      updatingSourceIds,
      loadError,
      createError,
      listSources,
      createSource,
      setSourceEnabled,
    ],
  );
}
