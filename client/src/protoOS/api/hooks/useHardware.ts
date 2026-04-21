import { useCallback, useEffect, useMemo, useState } from "react";

import { TOTAL_FAN_SLOTS, TOTAL_HASHBOARD_SLOTS, TOTAL_PSU_SLOTS } from "../constants";
import {
  ControlBoardInfo,
  FanInfo,
  HardwareInfoHardwareinfo,
  HashboardInfo,
  PsuInfo,
} from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";

const useHardware = () => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<HardwareInfoHardwareinfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const [controlBoardInfo, setControlBoardInfo] = useState<ControlBoardInfo | undefined>();
  const [hashboardsInfo, setHashboardsInfo] = useState<(HashboardInfo | null)[] | undefined>();
  const [psusInfo, setPsusInfo] = useState<(PsuInfo | null)[] | undefined>();
  const [fansInfo, setFansInfo] = useState<(FanInfo | null)[] | undefined>();

  const fetchHardware = useCallback(() => {
    if (!api) return;

    setPending(true);
    authRetry({
      request: (params) => api.getHardware(params),
      onSuccess: (res) => {
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
        const allHashboards = Array.from({ length: TOTAL_HASHBOARD_SLOTS }, (_, i) => {
          const slot = i + 1;
          return hashboardsBySlot.get(slot) || null;
        });
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
          if (fan.slot !== undefined) {
            fansBySlot.set(fan.slot, fan);
          }
        });
        const allFans = Array.from({ length: TOTAL_FAN_SLOTS }, (_, i) => {
          const slot = i + 1;
          return fansBySlot.get(slot) || null;
        });
        setFansInfo(allFans);
      },
      onError: (err) => setError(err?.error?.message ?? "An error occurred"),
    }).finally(() => {
      setPending(false);
    });
  }, [api, authRetry]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchHardware();
  }, [fetchHardware]);

  // Update hardware store with hashboard data
  useEffect(() => {
    if (!hashboardsInfo) return;

    // Populate MinerInfoStore with basic hashboard info
    const hashboardSerials: string[] = [];
    hashboardsInfo?.forEach((hb) => {
      if (hb?.hb_sn && hb?.slot) {
        const existingHashboard = useMinerStore.getState().hardware.getHashboard(hb.hb_sn);

        const hashboardData = {
          ...existingHashboard, // Preserve all existing data
          serial: hb.hb_sn,
          slot: hb.slot,
          board: hb.board,
          bay: Math.floor((hb.slot - 1) / 3) + 1, // TODO: this should come from API
          apiVersion: hb.api_version,
          chipId: hb.chip_id,
          port: hb.port,
          miningAsic: hb.mining_asic,
          miningAsicCount: hb.mining_asic_count,
          temperatureSensorCount: hb.temp_sensor_count,
          ecLogsPath: hb.ec_logs_path,
          firmware: hb.firmware
            ? {
                version: hb.firmware.version,
                build: hb.firmware.build,
                gitHash: hb.firmware.git_hash,
                imageHash: hb.firmware.image_hash,
              }
            : undefined,
          bootloader: hb.bootloader
            ? {
                version: hb.bootloader.version,
                build: hb.bootloader.build,
                gitHash: hb.bootloader.git_hash,
                imageHash: hb.bootloader.image_hash,
              }
            : undefined,
        };

        useMinerStore.getState().hardware.addHashboard(hashboardData);
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

  // Update hardware store with PSU data
  useEffect(() => {
    if (!psusInfo) return;

    psusInfo.forEach((psu) => {
      if (psu?.slot !== undefined) {
        // Update hardware store only - telemetry now comes from useTelemetry
        useMinerStore.getState().hardware.addPsu({
          id: psu.slot,
          serial: psu.psu_sn,
          slot: psu.slot,
          manufacturer: psu.manufacturer,
          model: psu.model,
          hwRevision: psu.hw_revision,
          firmware: psu.firmware
            ? {
                appVersion: psu.firmware.app_version,
                bootloaderVersion: psu.firmware.bootloader_version,
              }
            : undefined,
        });
      }
    });
  }, [psusInfo]);

  // Update hardware and telemetry stores with Fan data
  useEffect(() => {
    if (!fansInfo) return;

    fansInfo.forEach((fan) => {
      if (fan?.slot !== undefined) {
        // Update hardware store
        useMinerStore.getState().hardware.addFan({
          slot: fan.slot,
          name: fan.name,
        });

        // Update telemetry store with fan min/max RPM
        useMinerStore.getState().telemetry.updateFanTelemetry(fan.slot, {
          slot: fan.slot,
          minRpm:
            fan.min_rpm !== undefined
              ? {
                  latest: {
                    value: fan.min_rpm,
                    units: "RPM",
                  },
                }
              : undefined,
          maxRpm:
            fan.max_rpm !== undefined
              ? {
                  latest: {
                    value: fan.max_rpm,
                    units: "RPM",
                  },
                }
              : undefined,
        });
      }
    });
  }, [fansInfo]);

  // Update hardware store with control board data
  useEffect(() => {
    if (!controlBoardInfo) return;

    useMinerStore.getState().hardware.setControlBoard({
      serial: controlBoardInfo.serial_number,
      boardId: controlBoardInfo.board_id,
      machineName: controlBoardInfo.machine_name,
      firmware: controlBoardInfo.firmware
        ? {
            name: controlBoardInfo.firmware.name,
            version: controlBoardInfo.firmware.version,
            variant: controlBoardInfo.firmware.variant,
            gitHash: controlBoardInfo.firmware.git_hash,
            imageHash: controlBoardInfo.firmware.image_hash,
          }
        : undefined,
      mpu: controlBoardInfo.mpu
        ? {
            cpuArchitecture: controlBoardInfo.mpu.cpu_architecture,
            cpuImplementer: controlBoardInfo.mpu.cpu_implementer,
            cpuPart: controlBoardInfo.mpu.cpu_part,
            cpuRevision: controlBoardInfo.mpu.cpu_revision,
            cpuVariant: controlBoardInfo.mpu.cpu_variant,
            hardware: controlBoardInfo.mpu.hardware,
            modelName: controlBoardInfo.mpu.model_name,
            processor: controlBoardInfo.mpu.processor,
            revision: controlBoardInfo.mpu.revision,
          }
        : undefined,
    });
  }, [controlBoardInfo]);

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
    [pending, error, data, controlBoardInfo, hashboardsInfo, psusInfo, fansInfo],
  );
};

export { useHardware };
