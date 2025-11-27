import { useCallback, useEffect, useMemo, useState } from "react";

import { TOTAL_FAN_SLOTS } from "../constants";
import { usePoll } from "./usePoll";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { CoolingConfig, CoolingStatusCoolingstatus, FanStatus, HttpResponse } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors, useAuthHeader, useMinerStore } from "@/protoOS/store";

// Extended type to account for null fan statuses when slots are missing
export type CoolingStatusWithNullableFans = Omit<CoolingStatusCoolingstatus, "fans"> & {
  fans?: (FanStatus | null)[];
};

interface UseCoolingStatusProps {
  poll?: boolean;
}

interface SetCoolingProps {
  mode: CoolingConfig["mode"];
  onSuccess?: (res: HttpResponse<CoolingConfig>) => void;
  onError?: (error: ErrorProps) => void;
}

const useCoolingStatus = ({ poll }: UseCoolingStatusProps = {}) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<CoolingStatusWithNullableFans>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  const fetchData = useCallback(() => {
    if (!api) return;

    const performFetch = () => {
      setPending(true);
      api
        .getCooling()
        .then((res) => {
          const coolingData = res?.data["cooling-status"];

          // Fill out fans array with all slots
          if (coolingData) {
            const fans = coolingData.fans;
            const fansBySlot = new Map<number, FanStatus>();
            fans?.forEach((fan) => {
              if (fan.id !== undefined) {
                fansBySlot.set(fan.id, fan);
              }
            });
            const allFans = Array.from({ length: TOTAL_FAN_SLOTS }, (_, i) => {
              const slot = i + 1;
              return fansBySlot.get(slot) || null;
            });

            setData({
              ...coolingData,
              fans: allFans,
            });
          } else {
            setData(coolingData);
          }
          setPending(false);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: (error) => {
              setError(error?.error?.message ?? "An error occurred");
              setPending(false);
            },
            onSuccess: () => {
              // Retry fetch after successful token refresh
              performFetch();
            },
          });
        });
    };

    performFetch();
  }, [api, handleAuthErrors]);

  usePoll({
    fetchData,
    poll,
  });

  // Update telemetry store when data changes
  useEffect(() => {
    if (!data?.fans) return;

    data.fans.forEach((fan) => {
      if (fan?.id !== undefined) {
        useMinerStore.getState().telemetry.updateFanTelemetry(fan.id, {
          id: fan.id,
          rpm:
            fan.rpm !== undefined
              ? {
                  latest: {
                    value: fan.rpm,
                    units: "RPM",
                  },
                }
              : undefined,
          percentage:
            fan.percentage !== undefined
              ? {
                  latest: {
                    value: fan.percentage,
                    units: "%",
                  },
                }
              : undefined,
        });
      }
    });
  }, [data]);

  const setCooling = useCallback(
    async ({ mode, onSuccess, onError }: SetCoolingProps) => {
      if (!api) return;

      const performSetCooling = async () => {
        setPending(true);
        await api
          .setCoolingMode({ mode }, authHeader)
          .then(async (res) => {
            const data = await res.json();
            setData(data.mode);
            setPending(false);
            onSuccess?.(data);
          })
          .catch((error) => {
            handleAuthErrors({
              error,
              onError: (error) => {
                (onError?.(error), setPending(false));
              },
              onSuccess: () => {
                performSetCooling();
              },
            });
          });
      };

      await performSetCooling();
    },
    [api, authHeader, handleAuthErrors],
  );

  return useMemo(() => ({ pending, error, data, setCooling }), [pending, error, data, setCooling]);
};

export { useCoolingStatus };
