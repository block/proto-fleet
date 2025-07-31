import { useEffect, useMemo, useState } from "react";

import {
  ControlBoardInfo,
  FanInfoFaninfo,
  HardwareInfoHardwareinfo,
  HashboardsInfoHashboardsinfo,
  PsusInfoPsusinfo,
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
  const [psusInfo, setPsusInfo] = useState<PsusInfoPsusinfo[] | undefined>();
  const [fansInfo, setFansInfo] = useState<FanInfoFaninfo[] | undefined>();

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getHardware()
      .then((res) => {
        const responseData = res?.data["hardware-info"];
        setData(responseData);
        setControlBoardInfo(responseData?.["cb-info"]);
        setHashboardsInfo(responseData?.["hashboards-info"]);
        setPsusInfo(responseData?.["psus-info"]);
        setFansInfo(responseData?.["fans-info"]);
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
