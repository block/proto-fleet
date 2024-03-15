import { useCallback } from "react";
import { useNavigate } from "react-router-dom";

import { variants } from "components/Button";
import InfoWidget from "components/InfoWidget";
import Modal from "components/Modal";

import HashboardRow from "./HashboardRow";

interface AsicTempModalProps {
  avgAsicTemp?: string | number;
  onDismiss: () => void;
}

const AsicTempModal = ({ avgAsicTemp, onDismiss }: AsicTempModalProps) => {
  const navigate = useNavigate();

  const handleClickViewAsics = useCallback(() => {
    onDismiss();
    navigate("/hardware");
  }, [navigate, onDismiss]);

  return (
    <Modal
      buttons={[
        {
          text: "View ASICs",
          onClick: handleClickViewAsics,
          variant: variants.secondary,
        },
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
      contentHeader="ASIC Temperature"
      onDismiss={onDismiss}
    >
      <div className="space-y-6">
        <div>
          Proto ASICs are most performant around 50ºc - 90ºc and the miner will
          auto-tune to optimize performance. If temperatures go beyond 90ºc, the
          miner will no longer be able to mine.
        </div>
        <div className="flex">
          <InfoWidget title="Avg. ASIC Temp" value={avgAsicTemp} />
          {/* TODO: get highest temp when API provides it */}
          <InfoWidget title="Highest Temp" value="81.4°c" />
        </div>
        <div>
          {/* TODO: get temp for each hashboard when API provides it */}
          <HashboardRow
            label="Hashboard 1"
            secondaryLabel="75.56ºc • 12 chips are over heating"
            warn
          />
          <HashboardRow label="Hashboard 2" secondaryLabel="62.56ºc" />
          <HashboardRow
            label="Hashboard 3"
            secondaryLabel="68.72ºc"
            divider={false}
            className="-mb-4"
          />
        </div>
      </div>
    </Modal>
  );
};

export default AsicTempModal;
