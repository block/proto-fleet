import { useState } from "react";
import { maxNumberOfBackupPools } from "./constants";
import PoolsList from "./PoolsList/PoolsList";
import { MiningPool } from "./types";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Modal from "@/shared/components/Modal";
import { selectTypes } from "@/shared/constants";

interface MiningPoolsModalProps {
  numberOfMiners: number;
  availablePools: MiningPool[];
  onDismiss: (poolsChanged: boolean) => void;
}

// TODO save default and backup pools
// TODO handle add of default and backup pools
const PoolsModal = ({
  numberOfMiners,
  availablePools,
  onDismiss,
}: MiningPoolsModalProps) => {
  const [selectedDefaultPool, setSelectedDefaultPool] = useState<string | null>(
    null,
  );
  const [selectedBackupPools, setSelectedBackupPools] = useState<string[]>([]);
  // TODO improve change tracking?
  const [poolsChanged, setPoolsChanged] = useState(false);

  const handleSelectDefaultPool = (poolUrl: string, selected: boolean) => {
    setPoolsChanged(true);
    if (selected) {
      setSelectedDefaultPool(poolUrl);
    }
  };

  const handleSelectBackupPool = (poolUrl: string, checked: boolean) => {
    setPoolsChanged(true);
    setSelectedBackupPools((prev) => {
      if (checked && !prev.includes(poolUrl)) {
        return [poolUrl, ...prev].slice(0, maxNumberOfBackupPools);
      } else if (!checked) {
        return prev
          .filter((addr) => addr !== poolUrl)
          .slice(0, maxNumberOfBackupPools);
      }
      return prev;
    });
  };

  return (
    <Modal
      className="visible"
      divider={false}
      buttonSize={sizes.base}
      buttons={[
        !poolsChanged
          ? {
              text: "Done",
              variant: variants.primary,
            }
          : {
              text: "Update pools",
              variant: variants.accent,
            },
      ]}
      onDismiss={() => onDismiss(poolsChanged)}
    >
      <Header
        inline
        title="Mining pools"
        titleSize="text-heading-300"
        description={`Update the mining pools for ${numberOfMiners} miners.`}
      />
      <PoolsList
        title="Default pool"
        subtitle="Select one default pool"
        availablePools={availablePools}
        selectType={selectTypes.radio}
        selectedPools={
          selectedDefaultPool === null ? [] : [selectedDefaultPool]
        }
        onSelect={handleSelectDefaultPool}
        createNewLabel="Add default pool"
      />
      <PoolsList
        title="Backup pool"
        subtitle="Select up to two backup pools that we’ll use if your default pool is unavailable."
        availablePools={availablePools}
        selectType={selectTypes.checkbox}
        selectedPools={selectedBackupPools}
        onSelect={handleSelectBackupPool}
        createNewLabel="Add a backup pool"
      />
    </Modal>
  );
};

export default PoolsModal;
