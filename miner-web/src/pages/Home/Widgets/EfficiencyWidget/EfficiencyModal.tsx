import { variants } from "components/Button";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import EfficiencyChart from "./EfficiencyChart";

interface PowerUsageModalProps {
  efficiency?: string | number | null;
  onDismiss: () => void;
}

const EfficiencyModal = ({ efficiency, onDismiss }: PowerUsageModalProps) => (
  <Modal
    buttons={[
      {
        text: "Done",
        variant: variants.primary,
      },
    ]}
    contentHeader="Miner efficiency"
    onDismiss={onDismiss}
  >
    <div className="space-y-6">
      <div>Miner efficiency tracks the relationship between power usage and hashrate.</div>
      <div className="flex">
        {/* TODO: get average efficiency when API provides it */}
        <InfoWidget title="Avg. efficiency" value="12.5 J/TH" />
        <InfoWidget title="Current efficiency" value={efficiency} />
      </div>
      <div className="w-[600px] h-[228px]">
        <EfficiencyChart />
      </div>
    </div>
  </Modal>
);

export default EfficiencyModal;
