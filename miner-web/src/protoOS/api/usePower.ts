import { useCallback, useMemo, useState } from "react";

import { PowerResponsePowerdata } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UsePowerProps {
  duration: PowerResponsePowerdata["duration"];
  poll?: boolean;
}

const usePower = ({ duration, poll }: UsePowerProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<PowerResponsePowerdata>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getMinerPower({ duration })
      .then((res) => {
        setData(res?.data["power-data"]);
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

export { usePower };
