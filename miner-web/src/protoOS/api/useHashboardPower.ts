import { useCallback, useEffect, useMemo, useState } from "react";

import type { ErrorResponse, PowerResponsePowerdata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashboardPowerProps {
  duration: PowerResponsePowerdata["duration"];
  hashboardSerial: string | string[];
  poll?: boolean;
}

type MultiplePowerResponsePowerdata = {
  [key: string]: PowerResponsePowerdata;
};

const useHashboardPower = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardPowerProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<
    PowerResponsePowerdata | MultiplePowerResponsePowerdata
  >();
  const [errors, setErrors] = useState<string[]>([]);
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({ duration, hashboardSerial });

  const fetchSingle = useCallback(
    async (serial: string) => {
      if (!hashboardSerial || !api) return;
      setPending(true);

      try {
        const res = await api.getHashboardPower({
          hbSn: serial,
          duration,
        });

        return res?.data["power-data"];
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
      const powerData = await fetchSingle(hashboardSerial);
      setData(powerData);
      setPending(false);
      return;
    }

    const fetchPromises = hashboardSerial.map(async (serial) => {
      const powerData = await fetchSingle(serial);
      return { serial, powerData };
    });

    Promise.all(fetchPromises).then((results) => {
      // Update the data state with all results at once
      const newData = results.reduce((acc, { serial, powerData }) => {
        if (!powerData) return acc;
        return { ...acc, [serial]: powerData };
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

  const response = useMemo(
    () => ({ pending, errors, data }),
    [pending, errors, data],
  );

  return response;
};

export { useHashboardPower };
