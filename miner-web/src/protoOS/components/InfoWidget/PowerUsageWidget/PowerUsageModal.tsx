import PowerUsageChart from "./PowerUsageChart";
import { Aggregates } from "@/protoOS/api/types";

import InfoWidget from "@/protoOS/components/InfoWidget";
import { variants } from "@/shared/components/Button";

import { Duration } from "@/shared/components/DurationSelector";
import Modal from "@/shared/components/Modal";
import { getDisplayValue } from "@/shared/utils/stringUtils";

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
          <div className="h-[156px] w-[592px] phone:w-[352px]">
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
