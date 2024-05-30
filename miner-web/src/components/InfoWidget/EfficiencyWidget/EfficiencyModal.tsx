import { variants } from "components/Button";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import EfficiencyChart from "./EfficiencyChart";

interface PowerUsageModalProps {
  avgEfficiency?: string | number | null;
  efficiency?: string | number | null;
  efficiencyValues?: Record<string, number | string>[];
  onDismiss: () => void;
}

const EfficiencyModal = ({
  avgEfficiency,
  efficiency,
  efficiencyValues,
  onDismiss,
}: PowerUsageModalProps) => (
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
      <div>
        Miner efficiency tracks the relationship between power usage and
        hashrate.
      </div>
      <div className="flex">
        <InfoWidget title="Avg. efficiency" value={avgEfficiency && `${avgEfficiency} J/TH`} />
        <InfoWidget title="Current efficiency" value={efficiency && `${efficiency} J/TH`} />
      </div>
      {efficiencyValues && (
        <div className="w-[600px] phone:w-[352px] h-[228px]">
          <EfficiencyChart efficiencies={efficiencyValues} />
        </div>
      )}
    </div>
  </Modal>
);

export default EfficiencyModal;
