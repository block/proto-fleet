import Button, {
  sizes as buttonSizes,
  variants,
} from "@/shared/components/Button";
import Modal, { sizes as modalSizes } from "@/shared/components/Modal";

const ErrorModal = () => {
  return (
    <Modal showHeader={false} size={modalSizes.small}>
      <h2 className="text-heading-200 text-text-primary">
        There was an issue installing the firmware update.
      </h2>
      <p className="text-text-primary-200 mt-1 mb-4 text-300">
        If the issue persists, try rebooting the miner and then installing the
        firmware.
      </p>

      <div className="flex gap-2">
        <Button
          text="Cancel"
          variant={variants.secondary}
          size={buttonSizes.base}
          onClick={() => {}}
          className="grow"
        />
        <Button
          text="Try again"
          variant={variants.accent}
          size={buttonSizes.base}
          onClick={() => {}}
          className="grow"
        />
      </div>
    </Modal>
  );
};

export default ErrorModal;
