import { alertCols } from "./constants";
import { type Alert } from "./types";
import MinerStatus from "@/protoFleet/features/fleetManagement/components/MinerList/MinerStatus";
import { ColConfig } from "@/shared/components/List/types";
import { getRelativeTimeFromEpoch } from "@/shared/utils/datetime";

const alertColConfig: ColConfig<Alert, Alert["minerMacAddress"]> = {
  [alertCols.name]: {
    width: "w-24",
  },
  [alertCols.status]: {
    component: (item: Alert, selectedItems: Alert["minerMacAddress"][]) => {
      return <MinerStatus deviceIdentifier={item.minerMacAddress} selectedItems={selectedItems} />;
    },
    width: "w-17",
  },
  [alertCols.error]: {
    width: "w-86 phone:w-42",
  },
  [alertCols.timestamp]: {
    component: (item: Alert) => <div className="text-text-primary-50">{getRelativeTimeFromEpoch(item.timestamp)}</div>,
    width: "w-24",
  },
};

export default alertColConfig;
