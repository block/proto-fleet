import { useEffect, useMemo, useState } from "react";

import { HashboardInfo } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";

const useHashboards = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashboardInfo[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    if (!api) {
      return;
    }

    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPending(true);

    api
      .getAllHashboards()
      .then((res) => {
        const hashboards = res?.data["hashboards-info"];
        setData(hashboards);
      })
      .catch((err) => {
        setError(err?.error?.message ?? "An error occurred");
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  useEffect(() => {
    if (!data) return;
    // Populate MinerInfoStore with basic hashboard info
    const hashboardSerials: string[] = [];
    data?.forEach((hb) => {
      if (hb.hb_sn && hb.slot) {
        const existingHashboard = useMinerStore.getState().hardware.getHashboard(hb.hb_sn);
        if (!existingHashboard) {
          useMinerStore.getState().hardware.addHashboard({
            serial: hb.hb_sn,
            slot: hb.slot,
            board: hb.board,
            bay: Math.floor((hb.slot - 1) / 3) + 1,
            asicIds: [], // Will be populated later by useHashboardStatus
          });
        }
        hashboardSerials.push(hb.hb_sn);
      }
    });

    // Create or update the miner record with hashboard serials
    const existingMiner = useMinerStore.getState().hardware.getMiner();
    if (!existingMiner && hashboardSerials.length > 0) {
      useMinerStore.getState().hardware.setMiner({
        controlBoardSerial: "unknown", // TODO: Get from API when available
        hashboardSerials,
      });
    }
  }, [data]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboards };
