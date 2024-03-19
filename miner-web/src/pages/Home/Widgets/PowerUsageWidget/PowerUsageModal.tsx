import { variants } from "components/Button";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import PowerUsageChart from "./PowerUsageChart";

interface PowerUsageModalProps {
  onDismiss: () => void;
  powerUsage?: string | null;
}

const PowerUsageModal = ({ onDismiss, powerUsage }: PowerUsageModalProps) => (
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
        <InfoWidget title="Current power usage" value={powerUsage} />
        {/* TODO: get average power usage when API provides it */}
        <InfoWidget title="Avg. power usage" value="3.8 kW" />
      </div>
      <div className="w-[592px] h-[156px]">
        <PowerUsageChart />
      </div>
    </div>
  </Modal>
);

export default PowerUsageModal;
