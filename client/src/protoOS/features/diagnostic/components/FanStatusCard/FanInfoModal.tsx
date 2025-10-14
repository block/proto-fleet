import LabeledValue from "../LabeledValue";
import MetadataRow from "../MetadataRow";
import type { FanData } from "@/protoOS/features/diagnostic/types";
import { Fan } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";

interface FanInfoModalProps {
  fanData: FanData;
  onDismiss: () => void;
}

function FanInfoModal({ fanData, onDismiss }: FanInfoModalProps) {
  const formatRPM = (rpm: number) => {
    return rpm.toLocaleString() + " RPM";
  };

  const formatPWM = (pwm: number) => {
    return pwm.toFixed(1) + "%";
  };

  return (
    <Modal
      onDismiss={onDismiss}
      title="Fan status"
      size="large"
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: onDismiss,
        },
      ]}
    >
      <div className="flex flex-col gap-y-6 py-6">
        <Header
          icon={
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-core-primary-5">
              <Fan />
            </div>
          }
          title={fanData.name}
          titleSize="text-heading-300"
        />

        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <LabeledValue
            value={formatRPM(fanData.rpm)}
            label="Speed"
            variant="large"
          />
          <LabeledValue
            value={formatPWM(fanData.pwm)}
            label="PWM"
            variant="large"
          />
        </div>

        <div className="flex flex-col">
          {fanData.meta.serialNumber && (
            <MetadataRow
              label="Serial number"
              value={fanData.meta.serialNumber}
            />
          )}
          {fanData.meta.manufacturer && (
            <MetadataRow
              label="Manufacturer"
              value={fanData.meta.manufacturer}
            />
          )}
          {fanData.meta.model && (
            <MetadataRow label="Model" value={fanData.meta.model} />
          )}
          {fanData.meta.firmwareVersion && (
            <MetadataRow
              label="Firmware version"
              value={fanData.meta.firmwareVersion}
            />
          )}
        </div>
      </div>
    </Modal>
  );
}

export default FanInfoModal;
