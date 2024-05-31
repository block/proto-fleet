import { Aggregates } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import { variants } from "components/Button";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import PowerUsageChart from "./PowerUsageChart";

interface PowerUsageModalProps {
  onDismiss: () => void;
  powerAggregates?: Aggregates;
  powerUsage?: string | number | null;
  powerValues?: Record<string, number | string>[];
}

const PowerUsageModal = ({
  onDismiss,
  powerAggregates,
  powerUsage,
  powerValues,
}: PowerUsageModalProps) => (
  <Modal
    buttons={[
      {
        text: "Done",
        variant: variants.primary,
      },
    ]}
    contentHeader="Power usage"
    onDismiss={onDismiss}
  >
    <div className="space-y-6">
      <div>How much power this miner has been consuming.</div>
      <div className="flex">
        <InfoWidget
          title="Avg. power usage"
          value={
            powerAggregates?.avg && `${getDisplayValue(powerAggregates.avg)} kW`
          }
        />
        <InfoWidget
          title="Current power usage"
          value={powerUsage && `${getDisplayValue(powerUsage)} kW`}
        />
      </div>
      {powerValues && powerAggregates?.max && (
        <div className="w-[592px] phone:w-[352px] h-[156px]">
          <PowerUsageChart
            powers={powerValues}
            maxPower={powerAggregates.max}
          />
        </div>
      )}
    </div>
  </Modal>
);

export default PowerUsageModal;
