import { useCallback, useEffect, useMemo, useState } from "react";

import type { ErrorResponse, HashrateResponseHashratedata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashboardHashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  hashboardSerial: string | string[];
  poll?: boolean;
}

type MultipleHashrateResponseHashratedata = {
  [key: string]: HashrateResponseHashratedata;
};

const useHashboardHashrate = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardHashrateProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<
    HashrateResponseHashratedata | MultipleHashrateResponseHashratedata
  >();
  const [errors, setErrors] = useState<string[]>([]);
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({ duration, hashboardSerial });

  const fetchSingle = useCallback(
    async (serial: string) => {
      if (!hashboardSerial || !api) return;
      setPending(true);

      try {
        const res = await api.getHashboardHashrate({ hbSn: serial, duration });
        return res?.data["hashrate-data"];
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
      const hashRateData = await fetchSingle(hashboardSerial);
      setData(hashRateData);
      setPending(false);
      return;
    }

    const fetchPromises = hashboardSerial.map(async (serial) => {
      const hashRateData = await fetchSingle(serial);
      return { serial, hashRateData };
    });

    Promise.all(fetchPromises).then((results) => {
      // Update the data state with all results at once
      const newData = results.reduce((acc, { serial, hashRateData }) => {
        if (!hashRateData) return acc;
        return { ...acc, [serial]: hashRateData };
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

export { useHashboardHashrate };
