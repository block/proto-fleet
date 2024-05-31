import { EfficiencyResponseEfficiencydata } from "apiTypes";

import { getTimeFromEpoch } from "common/utils/stringUtils";

export const convertEfficiencyValues = (
  data: EfficiencyResponseEfficiencydata["data"]
) => {
  return data?.map((data) => ({
    time: getTimeFromEpoch(data.datetime),
    value: data.value || 0,
  }));
};
