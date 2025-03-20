import EfficiencyChart from "./EfficiencyChart";
import InfoWidget from "@/protoOS/components/InfoWidget";
import { variants } from "@/shared/components/Button";
import { Duration } from "@/shared/components/DurationSelector";
import Modal from "@/shared/components/Modal";

interface PowerUsageModalProps {
  avgEfficiency?: string | number | null;
  efficiency?: string | number | null;
  efficiencyValues?: Record<string, number | string>[];
  duration: Duration;
  onDismiss: () => void;
}

const EfficiencyModal = ({
  avgEfficiency,
  efficiency,
  efficiencyValues,
  duration,
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
        <InfoWidget
          title="Current efficiency"
          value={efficiency && `${efficiency} J/TH`}
        />
        <InfoWidget
          title={`${duration.toUpperCase()} avg. efficiency`}
          value={avgEfficiency && `${avgEfficiency} J/TH`}
        />
      </div>
      {efficiencyValues?.length ? (
        <div className="flex justify-center">
          <div className="h-[228px] w-[600px] phone:w-[352px]">
            <EfficiencyChart efficiencies={efficiencyValues} />
          </div>
        </div>
      ) : null}
    </div>
  </Modal>
);

export default EfficiencyModal;
