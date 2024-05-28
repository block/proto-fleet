import { useEffect } from "react";

interface UsePollProps {
  fetchData: () => void;
  poll?: boolean;
}

const usePoll = ({ fetchData, poll }: UsePollProps) => {
  useEffect(() => {
    fetchData();
    if (poll) {
      const interval = setInterval(fetchData, 60000);
      return () => clearInterval(interval);
    }
  }, [fetchData, poll]);
};

export { usePoll };
