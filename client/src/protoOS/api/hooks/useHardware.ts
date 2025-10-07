import { useEffect, useMemo, useState } from "react";

import {
  TOTAL_FAN_SLOTS,
  TOTAL_HASHBOARD_SLOTS,
  TOTAL_PSU_SLOTS,
} from "../constants";
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
    (HashboardInfo | null)[] | undefined
  >();
  const [psusInfo, setPsusInfo] = useState<(PsuInfo | null)[] | undefined>();
  const [fansInfo, setFansInfo] = useState<(FanInfo | null)[] | undefined>();

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getHardware()
      .then((res) => {
        const responseData = res?.data["hardware-info"];
        setData(responseData);
        setControlBoardInfo(responseData?.["cb-info"]);

        // Fill out hashboards array with all slots
        const hashboards = responseData?.["hashboards-info"];
        const hashboardsBySlot = new Map<number, HashboardInfo>();
        hashboards?.forEach((hb) => {
          if (hb.slot !== undefined) {
            hashboardsBySlot.set(hb.slot, hb);
          }
        });
        const allHashboards = Array.from(
          { length: TOTAL_HASHBOARD_SLOTS },
          (_, i) => {
            const slot = i + 1;
            return hashboardsBySlot.get(slot) || null;
          },
        );
        setHashboardsInfo(allHashboards);

        // Fill out PSUs array with all slots
        const psus = responseData?.["psus-info"];
        const psusBySlot = new Map<number, PsuInfo>();
        psus?.forEach((psu) => {
          if (psu.slot !== undefined) {
            psusBySlot.set(psu.slot, psu);
          }
        });
        const allPsus = Array.from({ length: TOTAL_PSU_SLOTS }, (_, i) => {
          const slot = i + 1;
          return psusBySlot.get(slot) || null;
        });
        setPsusInfo(allPsus);

        // Fill out fans array with all slots
        const fans = responseData?.["fans-info"];
        const fansBySlot = new Map<number, FanInfo>();
        fans?.forEach((fan) => {
          if (fan.id !== undefined) {
            fansBySlot.set(fan.id, fan);
          }
        });
        const allFans = Array.from({ length: TOTAL_FAN_SLOTS }, (_, i) => {
          const slot = i + 1;
          return fansBySlot.get(slot) || null;
        });
        setFansInfo(allFans);

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
      if (hb?.hb_sn && hb?.slot) {
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
