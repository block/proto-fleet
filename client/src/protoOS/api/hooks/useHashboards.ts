import { useEffect, useMemo, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import type { HashboardHardwareData } from "@/protoOS/store/types";

// Seed the store with the hashboards the miner reports. The adapter produces
// normalized descriptors (serial/slot/bay/board); this hook only writes slices.
const populateStore = (hashboards: HashboardHardwareData[]) => {
  const { hardware } = useMinerStore.getState();
  const serials: string[] = [];
  hashboards.forEach((hashboard) => {
    if (!hardware.getHashboard(hashboard.serial)) {
      hardware.addHashboard(hashboard);
    }
    serials.push(hashboard.serial);
  });

  if (!hardware.getMiner() && serials.length > 0) {
    hardware.setMiner({
      controlBoardSerial: "unknown", // TODO: Get from API when available
      hashboardSerials: serials,
    });
  }
};

const useHashboards = () => {
  const { adapter } = useMinerHosting();
  const [data, setData] = useState<HashboardHardwareData[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    if (!adapter) {
      return;
    }

    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    setPending(true);

    adapter
      .getHashboards()
      .then((hashboards) => {
        setData(hashboards);
        populateStore(hashboards);
      })
      .catch((err) => {
        setError(err?.error?.message ?? "An error occurred");
      })
      .finally(() => {
        setPending(false);
      });
  }, [adapter]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboards };
