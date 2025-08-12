import { useCallback, useEffect, useMemo, useState } from "react";

import type {
  ErrorResponse,
  TemperatureResponseTemperaturedata,
} from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { type Duration } from "@/shared/components/DurationSelector";

interface UseHashboardTemperatureProps {
  duration: Duration;
  hashboardSerial: string | string[];
  poll?: boolean;
}

type TemperatureResponseWithSerial = TemperatureResponseTemperaturedata & {
  hashboardSerial: string;
};

type MultipleTemperatureResponseTemperaturedata = {
  [key: string]: TemperatureResponseWithSerial;
};

const useHashboardTemperature = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardTemperatureProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<
    TemperatureResponseWithSerial | MultipleTemperatureResponseTemperaturedata
  >();
  const [errors, setErrors] = useState<string[]>([]);
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({ duration, hashboardSerial });

  const fetchSingle = useCallback(
    async (serial: string) => {
      if (!hashboardSerial || !api) return;
      setPending(true);

      try {
        const res = await api.getHashboardTemperature({
          hbSn: serial,
          duration,
        });

        return res?.data["temperature-data"];
      } catch (err) {
        const error = err as ErrorResponse;
        setErrors((prev) => [
          ...prev,
          error?.error?.message ?? error.toString(),
        ]);
      }
    },
    [duration, api, hashboardSerial],
  );

  const fetchData = useCallback(async () => {
    if (typeof hashboardSerial === "string") {
      const temperatureData = await fetchSingle(hashboardSerial);
      setData({ ...temperatureData, hashboardSerial: hashboardSerial });
      setPending(false);
      return;
    }

    const fetchPromises = hashboardSerial.map(async (serial) => {
      const temperatureData = await fetchSingle(serial);
      return { serial, temperatureData };
    });

    Promise.all(fetchPromises).then((results) => {
      // Update the data state with all results at once
      const newData = results.reduce((acc, { serial, temperatureData }) => {
        if (!temperatureData) return acc;
        return {
          ...acc,
          [serial]: { ...temperatureData, hashboardSerial: serial },
        };
      }, {});

      setData((prev) => ({ ...prev, ...newData }));
      setPending(false);
    });
  }, [fetchSingle, hashboardSerial]);

  useEffect(() => {
    if (
      duration !== params.duration ||
      hashboardSerial !== params.hashboardSerial
    ) {
      setParams({ duration, hashboardSerial });
    }
  }, [duration, hashboardSerial, params]);

  usePoll({
    fetchData,
    params,
    poll,
  });

  return useMemo(() => ({ pending, errors, data }), [pending, errors, data]);
};

export { useHashboardTemperature };
