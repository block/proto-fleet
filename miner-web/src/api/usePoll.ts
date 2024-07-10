import { useEffect } from "react";

interface UsePollProps {
  data: any;
  fetchData: () => void;
  pending: boolean;
  poll?: boolean;
  pollIntervalMs?: number;
}

const usePoll = ({
  data,
  fetchData,
  pending,
  poll,
  pollIntervalMs = 60000,
}: UsePollProps) => {
  useEffect(() => {
    if (!data && !pending) {
      fetchData();
      if (poll) {
        const interval = setInterval(fetchData, pollIntervalMs);
        return () => {
          clearInterval(interval);
        };
      }
    }
  // disable deps so we run and clear the interval only on mount/unmount
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
};

export { usePoll };
