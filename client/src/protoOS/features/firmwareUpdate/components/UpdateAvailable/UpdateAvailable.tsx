import { useState } from "react";
import clsx from "clsx";
import {
  InfoModal,
  InstallModal,
  useFirmwareUpdate,
} from "@/protoOS/features/firmwareUpdate";
import { Dismiss } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

type UpdateAvailableProps = {
  className?: string;
  dismiss: () => void;
};

const UpdateAvailable = ({ className, dismiss }: UpdateAvailableProps) => {
  const [showConfirmInstall, setShowConfirmInstall] = useState<boolean>(false);
  const [showInfoModal, setShowInfoModal] = useState<boolean>(false);
  const { version } = useFirmwareUpdate();

  return (
    <div
      className={clsx(
        "mx-2 flex flex-col rounded-3xl p-4 shadow-200",
        className,
      )}
    >
      <div className="mb-1 flex items-center justify-between">
        <StatusCircle status={statuses.pending} />
        <button className="p-0.5" onClick={dismiss}>
          <Dismiss
            width="w-3"
            className="text-text-primary-30 hover:text-text-primary-50"
          />
        </button>
      </div>
      <h4 className="text-heading-50">Firmware Update Available</h4>
      <p className="mb-3 text-200 text-text-primary-70">
        <a
          className="underline"
          href="#"
          onClick={(e) => {
            e.preventDefault();
            setShowInfoModal(true);
          }}
        >
          Learn more
        </a>{" "}
        about version {version}
      </p>
      <Button
        variant={variants.primary}
        size={sizes.compact}
        className="w-full"
        textColor="text-text-primary-30"
        text="Install"
        onClick={() => setShowConfirmInstall(true)}
      />
      {showConfirmInstall && (
        <InstallModal closeModal={() => setShowConfirmInstall(false)} />
      )}
      {showInfoModal && (
        <InfoModal closeModal={() => setShowInfoModal(false)} />
      )}
    </div>
  );
};

export default UpdateAvailable;
