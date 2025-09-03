import { useFirmwareUpdate } from "@/protoOS/api";
import { useFirmwareUpdateContext } from "@/protoOS/features/firmwareUpdate/contexts/FirmwareUpdateContext";
import Button, {
  sizes as buttonSizes,
  variants,
} from "@/shared/components/Button";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type InstallModalProps = {
  closeModal: () => void;
};

const InstallModal = ({ closeModal }: InstallModalProps) => {
  const { updateFirmware } = useFirmwareUpdate();
  const { installing } = useFirmwareUpdateContext();

  return (
    <Modal showHeader={false} size={modalSizes.small}>
      <h2 className="text-heading-200 text-text-primary">
        Install firmware update
      </h2>
      <p className="text-text-primary-200 mt-1 mb-4 text-300">
        Your miner will restart once the update has been installed. This will
        take 2-3 minutes.
      </p>

      <div className="flex gap-2">
        <Button
          text="Cancel"
          variant={variants.secondary}
          size={buttonSizes.base}
          onClick={closeModal}
          className="grow"
        />
        <Button
          text="Install"
          variant={variants.accent}
          size={buttonSizes.base}
          prefixIcon={
            installing ? (
              <ProgressCircular size={16} indeterminate />
            ) : undefined
          }
          disabled={installing}
          onClick={updateFirmware}
          className="grow"
        />
      </div>
    </Modal>
  );
};

export default InstallModal;
