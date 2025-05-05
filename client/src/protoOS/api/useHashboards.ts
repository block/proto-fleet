import { useEffect, useMemo, useState } from "react";

import { HashboardsInfoHashboardsinfo } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import useHashboardLocationStore, {
  type HashboardMap,
} from "@/protoOS/store/useHashboardLocationStore";

const useHashboards = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashboardsInfoHashboardsinfo[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const setMapping = useHashboardLocationStore((state) => state.setMapping);

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getAllHashboards()
      .then((res) => {
        const hashboards = res?.data["hashboards-info"];
        setData(hashboards);

        const mapping = hashboards?.reduce(
          (acc: HashboardMap, hb: HashboardsInfoHashboardsinfo) => {
            if (hb.hb_sn === undefined || hb.slot === undefined) {
              return acc;
            }

            acc[hb.hb_sn] = {
              slot: hb.slot,
              // TODO: directly use bay when it is available in the API response
              bay: Math.floor((hb.slot - 1) / 3) + 1,
            };
            return acc;
          },
          {},
        );

        if (mapping) {
          setMapping(mapping);
        }
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api, setMapping]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboards };
