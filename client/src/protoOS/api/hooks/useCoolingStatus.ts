import { useCallback, useEffect, useMemo, useState } from "react";

import { TOTAL_FAN_SLOTS } from "../constants";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { CoolingConfig, CoolingStatusCoolingstatus, FanStatus, HttpResponse } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry, useMinerStore } from "@/protoOS/store";
import { usePoll } from "@/shared/hooks/usePoll";

// Extended type to account for null fan statuses when slots are missing
export type CoolingStatusWithNullableFans = Omit<CoolingStatusCoolingstatus, "fans"> & {
  fans?: (FanStatus | null)[];
};

interface UseCoolingStatusProps {
  poll?: boolean;
  enabled?: boolean;
}

interface SetCoolingProps {
  mode: CoolingConfig["mode"];
  onSuccess?: (res: HttpResponse<CoolingConfig>) => void;
  onError?: (error: ErrorProps) => void;
}

const useCoolingStatus = ({ poll, enabled = true }: UseCoolingStatusProps = {}) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<CoolingStatusWithNullableFans>();
  const [error, setError] = useState<string>();
  const [loaded, setLoaded] = useState<boolean>(false);
  const [pending, setPending] = useState<boolean>(false);
  const authRetry = useAuthRetry();

  const fetchData = useCallback(() => {
    if (!api) return;

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
            if (fan.slot !== undefined) {
              fansBySlot.set(fan.slot, fan);
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

          // Update cooling mode in store
          if (coolingData.fan_mode) {
            useMinerStore.getState().telemetry.updateCoolingMode(coolingData.fan_mode);
          }
        } else {
          setData(coolingData);
        }
        setLoaded(true);
        setPending(false);
      })
      .catch((err) => {
        setError(err?.error?.message ?? "An error occurred");
        setLoaded(true);
        setPending(false);
      });
  }, [api]);

  usePoll({
    fetchData,
    enabled,
    poll,
  });

  // Update telemetry store when data changes
  useEffect(() => {
    if (!data?.fans) return;

    data.fans.forEach((fan) => {
      if (fan?.slot !== undefined) {
        useMinerStore.getState().telemetry.updateFanTelemetry(fan.slot, {
          slot: fan.slot,
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

      setPending(true);
      await authRetry({
        request: (header) => api.setCoolingMode({ mode }, header),
        onSuccess: async (res) => {
          const responseData = await res.json();
          setPending(false);

          if (mode !== undefined && mode !== null) {
            setData((prevData) => {
              if (!prevData) return prevData;
              return {
                ...prevData,
                fan_mode: mode,
              } as CoolingStatusWithNullableFans;
            });

            useMinerStore.getState().telemetry.updateCoolingMode(mode);
          }

          onSuccess?.(responseData);
        },
        onError: (error) => {
          onError?.(error);
          setPending(false);
        },
      });
    },
    [api, authRetry],
  );

  return useMemo(() => ({ pending, error, data, loaded, setCooling }), [pending, error, data, loaded, setCooling]);
};

export { useCoolingStatus };
