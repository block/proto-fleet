import { useEffect } from "react";

interface UsePollProps {
  fetchData: () => void;
  params?: any;
  poll?: boolean;
  pollIntervalMs?: number;
}

const usePoll = ({
  fetchData,
  params,
  poll,
  pollIntervalMs = 10 * 1000,
}: UsePollProps) => {
  useEffect(() => {
    fetchData();
    if (poll) {
      const interval = setInterval(fetchData, pollIntervalMs);
      return () => {
        clearInterval(interval);
      };
    }
    // disable deps so we run and clear the interval only on mount/unmount and when the params change
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);
};

export { usePoll };
