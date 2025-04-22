import { useCallback, useEffect, useMemo, useState } from "react";

import type { EfficiencyResponseEfficiencydata, ErrorResponse } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashboardEfficiencyProps {
  duration: EfficiencyResponseEfficiencydata["duration"];
  hashboardSerial: string | string[];
  poll?: boolean;
}

type MultipleEfficiencyResponseEfficiencydata = {
  [key: string]: EfficiencyResponseEfficiencydata;
};

const useHashboardEfficiency = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardEfficiencyProps) => {
  const { api } = useMinerHosting();

  const [data, setData] = useState<
    EfficiencyResponseEfficiencydata | MultipleEfficiencyResponseEfficiencydata
  >();
  const [errors, setErrors] = useState<string[]>([]);
  const [pending, setPending] = useState<boolean>(false);
  const [params, setParams] = useState({ duration, hashboardSerial });

  const fetchSingle = useCallback(
    async (serial: string) => {
      if (!hashboardSerial || !api) return;
      setPending(true);

      try {
        const res = await api.getHashboardEfficiency({
          hbSn: serial,
          duration,
        });

        return res?.data["efficiency-data"];
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
      const efficiencyData = await fetchSingle(hashboardSerial);
      setData(efficiencyData);
      setPending(false);
      return;
    }

    const fetchPromises = hashboardSerial.map(async (serial) => {
      const efficiencyData = await fetchSingle(serial);
      return { serial, efficiencyData };
    });

    Promise.all(fetchPromises).then((results) => {
      // Update the data state with all results at once
      const newData = results.reduce((acc, { serial, efficiencyData }) => {
        if (!efficiencyData) return acc;
        return { ...acc, [serial]: efficiencyData };
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

export { useHashboardEfficiency };
