import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  type ComponentStatusUpdate,
  ComponentStatusUpdate_Component,
  DataMode,
  type ListMinerStateSnapshotsRequest,
  type ListMinerStateSnapshotsResponse,
  MeasurementConfig_MeasurementType,
  type MeasurementUpdate,
  MinerComponentStatus,
  type MinerStateSnapshot,
  StreamMinerUpdatesRequestSchema,
  type StreamMinerUpdatesResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";

type FetchPairedMinersArgs = {
  pageSize?: ListMinerStateSnapshotsRequest["pageSize"];
  cursor?: ListMinerStateSnapshotsRequest["cursor"];
};

function updateMeasurement(
  measurementUpdate: MeasurementUpdate,
  minerToUpdate: MinerStateSnapshot,
) {
  const minerClone = { ...minerToUpdate };
  const type = measurementUpdate.measurementType;
  const measurement = measurementUpdate.measurement;

  if (!measurement) return false;

  const measurementTypeToProperty = {
    [MeasurementConfig_MeasurementType.HASHRATE]: "hashrate",
    [MeasurementConfig_MeasurementType.POWER_USAGE]: "powerUsage",
    [MeasurementConfig_MeasurementType.TEMPERATURE]: "temperature",
    [MeasurementConfig_MeasurementType.EFFICIENCY]: "efficiency",
  } as {
    [key in MeasurementConfig_MeasurementType]: keyof Pick<
      MinerStateSnapshot,
      "hashrate" | "powerUsage" | "temperature" | "efficiency"
    >;
  };

  const propertyName = measurementTypeToProperty[type];

  if (propertyName) {
    const currentValues = minerClone[propertyName];

    if (currentValues && currentValues.length > 0) {
      minerClone[propertyName] = [measurement, ...currentValues.slice(0, -1)];
    } else {
      minerClone[propertyName] = [measurement];
    }
  }

  return minerClone;
}

function updateStatus(
  { status, component }: ComponentStatusUpdate,
  minerToUpdate: MinerStateSnapshot,
) {
  const minerClone = { ...minerToUpdate };
  if (!minerClone.status) {
    minerClone.status = {
      controlBoard: 0,
      fans: 0,
      hashBoards: 0,
      psu: 0,
    } as MinerComponentStatus;
  }

  const updatedStatus = { ...minerClone.status };
  const componentToProperty = {
    [ComponentStatusUpdate_Component.CONTROL_BOARD]: "controlBoard",
    [ComponentStatusUpdate_Component.FANS]: "fans",
    [ComponentStatusUpdate_Component.HASH_BOARDS]: "hashBoards",
    [ComponentStatusUpdate_Component.PSU]: "psu",
  } as {
    [key in ComponentStatusUpdate_Component]: keyof Pick<
      MinerComponentStatus,
      "controlBoard" | "fans" | "hashBoards" | "psu"
    >;
  };

  const propertyName = componentToProperty[component];
  if (propertyName) {
    updatedStatus[propertyName] = status;
  }

  minerClone.status = updatedStatus;
  return minerClone;
}

const useFleet = () => {
  const { authTokens } = useAuthContext();

  const [miners, setMiners] = useState<
    ListMinerStateSnapshotsResponse["miners"]
  >([]);
  const [cursor, setCursor] =
    useState<ListMinerStateSnapshotsResponse["cursor"]>("");
  const [isStreaming, setIsStreaming] = useState(false);
  const streamAbortController = useRef<AbortController | null>(null);

  const updateMinerState = useCallback(
    (response: StreamMinerUpdatesResponse) => {
      const deviceId = response.deviceIdentifier;

      if (
        !deviceId ||
        !response.update ||
        response.update.case === "heartbeat"
      ) {
        return;
      }

      setMiners((currentMiners) => {
        const minerIndex = currentMiners.findIndex(
          (miner) => miner.deviceIdentifier === deviceId,
        );

        if (minerIndex === -1) {
          return currentMiners;
        }

        const updatedMiners = [...currentMiners];
        let minerToUpdate: MinerStateSnapshot = {
          ...updatedMiners[minerIndex],
        };

        if (response.update.case === "measurement") {
          minerToUpdate =
            updateMeasurement(response.update.value, minerToUpdate) ||
            currentMiners[minerIndex];
        } else if (response.update.case === "status") {
          minerToUpdate = updateStatus(response.update.value, minerToUpdate);
        }

        if (response.timestamp) {
          minerToUpdate.timestamp = response.timestamp;
        }

        updatedMiners[minerIndex] = minerToUpdate;
        return updatedMiners;
      });
    },
    [],
  );

  const startStreamingUpdates = useCallback(
    (deviceIdentifiers: string[]) => {
      if (!deviceIdentifiers || deviceIdentifiers.length === 0) {
        return;
      }

      if (streamAbortController.current) {
        streamAbortController.current.abort();
      }

      streamAbortController.current = new AbortController();

      setIsStreaming(true);

      (async () => {
        try {
          const request = create(StreamMinerUpdatesRequestSchema, {
            deviceIdentifiers,
            measurementTypes: [
              MeasurementConfig_MeasurementType.HASHRATE,
              MeasurementConfig_MeasurementType.POWER_USAGE,
              MeasurementConfig_MeasurementType.TEMPERATURE,
              MeasurementConfig_MeasurementType.EFFICIENCY,
            ],
            includeStatusUpdates: true,
            heartbeatIntervalSeconds: 30,
          });

          for await (const response of fleetManagementClient.streamMinerUpdates(
            request,
            {
              ...getAuthHeader(authTokens),
              signal: streamAbortController.current?.signal,
            },
          )) {
            updateMinerState(response);
          }
        } catch (error) {
          const errorMessage = String(error);

          // Check if the error is due to an aborted request
          // ConnectError with 'canceled' or AbortError means the request was intentionally aborted
          if (
            errorMessage.includes("[canceled]") ||
            errorMessage.includes("AbortError") ||
            (streamAbortController.current &&
              streamAbortController.current.signal.aborted)
          ) {
            return;
          }

          console.error("Error streaming miner updates:", error);
        } finally {
          setIsStreaming(false);
        }
      })();
    },
    [authTokens, updateMinerState],
  );

  const fetchPairedMiners = useCallback(
    async ({ pageSize }: FetchPairedMinersArgs) => {
      try {
        const response = await fleetManagementClient.listMinerStateSnapshots(
          {
            pageSize,
            cursor,
            measurementConfigs: [
              {
                measurementType: MeasurementConfig_MeasurementType.HASHRATE,
                dataMode: DataMode.TIME_SERIES,
                timeSeriesConfig: {
                  timeSelection: {
                    case: "lookbackPeriod",
                    value: {
                      seconds: BigInt(600),
                      nanos: 0,
                    },
                  },
                  resolution: 100,
                },
              },
            ],
          },
          getAuthHeader(authTokens),
        );

        const { miners, cursor: newCursor } = response;
        setMiners(miners);
        setCursor(newCursor);

        // Start streaming updates for these miners
        if (miners.length > 0) {
          const deviceIds = miners.map((miner) => miner.deviceIdentifier);
          startStreamingUpdates(deviceIds);
        }
      } catch (error) {
        console.error("Error fetching fleet data:", error);
        throw error;
      }
    },
    [cursor, authTokens, startStreamingUpdates],
  );

  useEffect(() => {
    fetchPairedMiners({ pageSize: 100 });

    // Clean up streaming when component unmounts
    return () => {
      if (streamAbortController.current) {
        streamAbortController.current.abort();
        streamAbortController.current = null;
      }
    };
  }, [fetchPairedMiners]);

  return useMemo(
    () => ({
      miners,
      isStreaming,
      loadMoreMiners: () => fetchPairedMiners({ pageSize: 100 }),
    }),
    [miners, isStreaming, fetchPairedMiners],
  );
};

export default useFleet;
