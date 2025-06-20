import { useEffect, useMemo } from "react";
import {
  statuses,
  useFirmwareUpdate,
} from "@/protoOS/features/firmwareUpdate/contexts/FirmwareUpdateContext";
import { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

type InfoModalProps = {
  closeModal: () => void;
};

const InfoModal = ({ closeModal }: InfoModalProps) => {
  const { version, changelog, status, pending, updateFirmware } =
    useFirmwareUpdate();
  const isInstalling = useMemo(() => {
    return (
      (status !== statuses.current && status !== statuses.available) || pending
    );
  }, [status, pending]);

  const installButton = useMemo(
    () => ({
      text: "Install",
      variant: variants.primary,
      disabled: isInstalling,
      prefixIcon: isInstalling ? (
        <ProgressCircular size={16} indeterminate />
      ) : undefined,
      dismissModalOnClick: false,
      onClick: updateFirmware,
    }),
    [isInstalling, updateFirmware],
  );

  useEffect(() => {
    if (status !== statuses.current && status !== statuses.available) {
      closeModal();
    }
  }, [status, closeModal]);

  return (
    <Modal
      divider={false}
      buttons={[installButton]}
      buttonSize={sizes.base}
      onDismiss={closeModal}
    >
      <h2 className="text-200 text-text-primary-70">Version {version}</h2>
      <p className="mt-1 mb-4 text-emphasis-300 text-text-primary">
        {changelog?.split("\n").map((line, index) => (
          <span key={index}>
            {line}
            <br />
          </span>
        ))}
      </p>
    </Modal>
  );
};

export default InfoModal;
