import { useEffect, useMemo, useState } from "react";

import {
  ControlBoardInfo,
  FanInfo,
  HardwareInfoHardwareinfo,
  HashboardInfo,
  PsuInfo,
} from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";

const useHardware = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HardwareInfoHardwareinfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const [controlBoardInfo, setControlBoardInfo] = useState<
    ControlBoardInfo | undefined
  >();
  const [hashboardsInfo, setHashboardsInfo] = useState<
    HashboardInfo[] | undefined
  >();
  const [psusInfo, setPsusInfo] = useState<PsuInfo[] | undefined>();
  const [fansInfo, setFansInfo] = useState<FanInfo[] | undefined>();

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getHardware()
      .then((res) => {
        const responseData = res?.data["hardware-info"];
        setData(responseData);
        setControlBoardInfo(responseData?.["cb-info"]);
        const hashboards = responseData?.["hashboards-info"];
        setHashboardsInfo(hashboards);
        setPsusInfo(responseData?.["psus-info"]);
        setFansInfo(responseData?.["fans-info"]);

        // Update hardware store with hashboard board field
        if (hashboards) {
          hashboards.forEach((hb) => {
            if (hb.hb_sn) {
              const existingHashboard = useMinerStore
                .getState()
                .hardware.getHashboard(hb.hb_sn);
              if (existingHashboard) {
                // Update existing hashboard with board info
                useMinerStore.getState().hardware.addHashboard({
                  ...existingHashboard,
                  board: hb.board, // Add board field from getHardware API
                });
              } else {
                // Create new hashboard with available info
                useMinerStore.getState().hardware.addHashboard({
                  serial: hb.hb_sn,
                  slot: hb.slot || 1,

                  // TODO: [STORE_REFACTOR] can we get bay from the api directly?
                  bay: Math.floor(((hb.slot || 1) - 1) / 3) + 1,
                  board: hb.board,
                  asicIds: [], // Will be populated by useHashboardStatus
                });
              }
            }
          });
        }
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  useEffect(() => {
    if (!hashboardsInfo) return;

    // Populate MinerInfoStore with basic hashboard info
    const hashboardSerials: string[] = [];
    hashboardsInfo?.forEach((hb) => {
      if (hb.hb_sn && hb.slot) {
        const existingHashboard = useMinerStore
          .getState()
          .hardware.getHashboard(hb.hb_sn);
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
        hashboardSerials,
      });
    }
  }, [hashboardsInfo]);

  return useMemo(
    () => ({
      pending,
      error,
      data,
      controlBoardInfo,
      hashboardsInfo,
      psusInfo,
      fansInfo,
    }),
    [
      pending,
      error,
      data,
      controlBoardInfo,
      hashboardsInfo,
      psusInfo,
      fansInfo,
    ],
  );
};

export { useHardware };
