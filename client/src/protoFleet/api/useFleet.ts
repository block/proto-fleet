import { useCallback, useEffect, useRef } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  DataMode,
  type ListMinerStateSnapshotsRequest,
  MeasurementConfig_MeasurementType,
  MinerListFilter,
  StreamMinerUpdatesRequestSchema,
  type StreamMinerUpdatesResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  getAuthHeader,
  useAuthContext,
} from "@/protoFleet/features/auth/contexts/AuthContext";
import {
  useFleetStore,
  useMinerIds,
} from "@/protoFleet/features/fleetManagement/store/useFleetStore";

type FetchPairedMinersArgs = {
  pageSize?: ListMinerStateSnapshotsRequest["pageSize"];
  cursor?: ListMinerStateSnapshotsRequest["cursor"];
  filter?: MinerListFilter;
};

const useFleet = () => {
  const { authTokens } = useAuthContext();

  const minerIds = useMinerIds();
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

      if (response.update.case === "measurement") {
        useFleetStore
          .getState()
          .updateMinerMeasurement(deviceId, response.update.value);
      } else if (response.update.case === "status") {
        // TODO do we want to refetch the whole list when some filters are specified and the status changes?
        useFleetStore
          .getState()
          .updateMinerStatus(deviceId, response.update.value);
      }

      if (response.timestamp) {
        useFleetStore
          .getState()
          .updateMinerTimestamp(deviceId, response.timestamp);
      }
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

      useFleetStore.getState().setStreaming(true);

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
          useFleetStore.getState().setStreaming(false);
        }
      })();
    },
    [authTokens, updateMinerState],
  );

  const fetchPairedMiners = useCallback(
    async ({ pageSize = 100, filter }: FetchPairedMinersArgs) => {
      try {
        const response = await fleetManagementClient.listMinerStateSnapshots(
          {
            pageSize,
            cursor: useFleetStore.getState().cursor,
            filter,
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

        const {
          miners,
          cursor: newCursor,
          totalMiners,
          totalStateCounts,
        } = response;
        useFleetStore.getState().setMiners(miners);
        useFleetStore.getState().setCursor(newCursor);
        useFleetStore.getState().setTotalMiners(totalMiners);
        if (totalStateCounts) {
          useFleetStore.getState().setMinerStateCounts(totalStateCounts);
        }

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
    [authTokens, startStreamingUpdates],
  );

  useEffect(() => {
    fetchPairedMiners({});

    // Clean up streaming when component unmounts
    return () => {
      if (streamAbortController.current) {
        streamAbortController.current.abort();
        streamAbortController.current = null;
      }
    };
  }, [fetchPairedMiners]);

  return {
    minerIds,
    fetchPairedMiners,
  };
};

export default useFleet;
