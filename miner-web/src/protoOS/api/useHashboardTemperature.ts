import { useCallback, useEffect, useMemo, useState } from "react";

import type {
  ErrorResponse,
  PowerResponsePowerdata,
  TemperatureResponseTemperaturedata,
} from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashboardTemperatureProps {
  duration: PowerResponsePowerdata["duration"];
  hashboardSerial: string | string[];
  poll?: boolean;
}

type MultipleTemperatureResponseTemperaturedata = {
  [key: string]: TemperatureResponseTemperaturedata;
};

const useHashboardTemperature = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardTemperatureProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<
    | TemperatureResponseTemperaturedata
    | MultipleTemperatureResponseTemperaturedata
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
      setData(temperatureData);
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
        return { ...acc, [serial]: temperatureData };
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
