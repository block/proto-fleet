import { useCallback, useMemo, useState } from "react";

import { TemperatureResponseTemperaturedata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { type Duration } from "@/shared/components/DurationSelector";

interface UseTemperatureProps {
  duration: Duration;
  poll?: boolean;
}

const useTemperature = ({ duration, poll }: UseTemperatureProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<TemperatureResponseTemperaturedata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getMinerTemperature({ duration })
      .then((res) => {
        setData(res?.data["temperature-data"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, api]);

  usePoll({
    fetchData,
    params: duration,
    poll,
  });

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useTemperature };
