import { Aggregates } from "apiTypes";

import { getDisplayValue } from "common/utils/stringUtils";

import { variants } from "components/Button";
import { Duration } from "components/DurationSelector";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import PowerUsageChart from "./PowerUsageChart";

interface PowerUsageModalProps {
  duration: Duration;
  onDismiss: () => void;
  powerAggregates?: Aggregates;
  powerUsage?: string | number | null;
  powerValues?: Record<string, number | string>[];
}

const PowerUsageModal = ({
  duration,
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
          title="Current power usage"
          value={powerUsage && `${getDisplayValue(powerUsage)} kW`}
        />
        <InfoWidget
          title={`${duration.toUpperCase()} avg. power usage`}
          value={
            powerAggregates?.avg && `${getDisplayValue(powerAggregates.avg)} kW`
          }
        />
      </div>
      {powerValues && powerAggregates?.max && (
        <div className="flex justify-center">
          <div className="w-[592px] phone:w-[352px] h-[156px]">
            <PowerUsageChart
              powers={powerValues}
              maxPower={powerAggregates.max}
            />
          </div>
        </div>
      )}
    </div>
  </Modal>
);

export default PowerUsageModal;
