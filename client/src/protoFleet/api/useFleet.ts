import { useCallback, useEffect, useState } from "react";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  type ListPairedMinersRequest,
  type ListPairedMinersResponse,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type FetchPairedMinersArgs = {
  pageSize?: ListPairedMinersRequest["pageSize"];
  cursor?: ListPairedMinersRequest["cursor"];
};

const useFleet = () => {
  const accessToken = localStorage.getItem("accessToken");
  const [miners, setMiners] = useState<ListPairedMinersResponse["miners"]>([]);
  const [cursor, setCursor] = useState<ListPairedMinersResponse["cursor"]>("");
  const [totalMiners, setTotalMiners] =
    useState<ListPairedMinersResponse["totalMiners"]>();

  void totalMiners; // not using this yet, but keeping it for potential future use

  const fetchPairedMiners = useCallback(
    async ({ pageSize }: FetchPairedMinersArgs) => {
      try {
        const response = await fleetManagementClient.listPairedMiners(
          { pageSize, cursor },
          {
            headers: {
              Authorization: `Bearer ${accessToken && JSON.parse(accessToken).value}`,
            },
          },
        );

        const { miners, cursor: newCursor, totalMiners } = response;
        setMiners(miners);
        setCursor(newCursor);
        setTotalMiners(totalMiners);
      } catch (error) {
        console.error("Error fetching fleet data:", error);
        throw error;
      }
    },
    [cursor, accessToken, setMiners, setCursor, setTotalMiners],
  );

  useEffect(() => {
    fetchPairedMiners({ pageSize: 100 });
  }, [fetchPairedMiners]);

  return miners;
};

export default useFleet;
