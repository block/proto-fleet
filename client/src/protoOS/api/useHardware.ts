import { useEffect, useMemo, useState } from "react";

import {
  ControlBoardInfo,
  FansInfo,
  HardwareInfoHardwareinfo,
  HashboardsInfoHashboardsinfo,
  PsusInfo,
} from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const useHardware = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HardwareInfoHardwareinfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const [controlBoardInfo, setControlBoardInfo] = useState<
    ControlBoardInfo | undefined
  >();
  const [hashboardsInfo, setHashboardsInfo] = useState<
    HashboardsInfoHashboardsinfo[] | undefined
  >();
  const [psusInfo, setPsusInfo] = useState<PsusInfo | undefined>();
  const [fansInfo, setFansInfo] = useState<FansInfo | undefined>();

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getHardware()
      .then((res) => {
        const responseData = res?.data["hardware-info"];
        setData(responseData);
        setControlBoardInfo(responseData?.["cb-info"]);
        // Extract actual arrays from wrapper objects
        const hashboards = responseData?.["hashboards-info"];
        setHashboardsInfo(
          Array.isArray(hashboards)
            ? hashboards.flatMap((h) => h["hashboards-info"] || [])
            : undefined,
        );

        const psus = responseData?.["psus-info"];
        setPsusInfo(Array.isArray(psus) ? psus.flat() : undefined);

        const fans = responseData?.["fans-info"];
        setFansInfo(Array.isArray(fans) ? fans.flat() : undefined);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

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
