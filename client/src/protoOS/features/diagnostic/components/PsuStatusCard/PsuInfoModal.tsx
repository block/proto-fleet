import LabeledValue from "../LabeledValue";
import MetadataRow from "../MetadataRow";
import type { PsuData } from "@/protoOS/features/diagnostic/types";
import { LightningAlt } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import PowerValue from "@/shared/components/PowerValue";
import TemperatureValue from "@/shared/components/TemperatureValue";
import VoltageValue from "@/shared/components/VoltageValue";

interface PsuInfoModalProps {
  psuData: PsuData;
  onDismiss: () => void;
}

function PsuInfoModal({ psuData, onDismiss }: PsuInfoModalProps) {
  return (
    <Modal
      onDismiss={onDismiss}
      title="PSU status"
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
              <LightningAlt />
            </div>
          }
          title={psuData.name}
          titleSize="text-heading-300"
        />

        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <LabeledValue
            value={<VoltageValue value={psuData.inputVoltage} />}
            label="Input voltage"
            variant="large"
          />
          <LabeledValue
            value={<VoltageValue value={psuData.outputVoltage} />}
            label="Output voltage"
            variant="large"
          />
          <LabeledValue
            value={<PowerValue value={psuData.inputPower} />}
            label="Input power"
            variant="large"
          />
          <LabeledValue
            value={<PowerValue value={psuData.outputPower} />}
            label="Output power"
            variant="large"
          />
          <LabeledValue
            value={<TemperatureValue value={psuData.avgTemp} />}
            label="Avg temp"
            variant="large"
          />
          <LabeledValue
            value={<TemperatureValue value={psuData.maxTemp} />}
            label="High temp"
            variant="large"
          />
        </div>

        <div className="flex flex-col">
          {psuData.meta.serialNumber && (
            <MetadataRow
              label="Serial number"
              value={psuData.meta.serialNumber}
            />
          )}
          {psuData.meta.manufacturer && (
            <MetadataRow
              label="Manufacturer"
              value={psuData.meta.manufacturer}
            />
          )}
          {psuData.meta.model && (
            <MetadataRow label="Model" value={psuData.meta.model} />
          )}
          {psuData.meta.vendor && (
            <MetadataRow label="Vendor" value={psuData.meta.vendor} />
          )}
          {psuData.meta.hardwareRevision && (
            <MetadataRow
              label="Hardware revision"
              value={psuData.meta.hardwareRevision}
            />
          )}
          {psuData.meta.firmwareAppVersion && (
            <MetadataRow
              label="Firmware app version"
              value={psuData.meta.firmwareAppVersion}
            />
          )}
          {psuData.meta.firmwareBootloaderVersion && (
            <MetadataRow
              label="Firmware bootloader version"
              value={psuData.meta.firmwareBootloaderVersion}
            />
          )}
        </div>
      </div>
    </Modal>
  );
}

export default PsuInfoModal;
