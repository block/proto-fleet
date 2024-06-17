import { useEffect } from "react";

interface UsePollProps {
  fetchData: () => void;
  poll?: boolean;
  pollIntervalMilliseconds?: number;
}

const usePoll = ({ fetchData, poll, pollIntervalMilliseconds = 60000 }: UsePollProps) => {
  useEffect(() => {
    fetchData();
    if (poll) {
      const interval = setInterval(fetchData, pollIntervalMilliseconds);
      return () => clearInterval(interval);
    }
  }, [fetchData, poll, pollIntervalMilliseconds]);
};

export { usePoll };
