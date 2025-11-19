import { useState } from "react";
import PoolsList from "./PoolsList/PoolsList";
import { MiningPool } from "./types";
import { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

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
  const [_selectedDefaultPool, setSelectedDefaultPool] = useState<
    string | null
  >(null);
  const [_selectedBackupPools, setSelectedBackupPools] = useState<string[]>([]);
  // TODO improve change tracking?
  const [poolsChanged, setPoolsChanged] = useState(false);

  const handleSelectDefaultPool = (poolUrl: string) => {
    setPoolsChanged(true);
    setSelectedDefaultPool(poolUrl);
  };

  const handleSelectBackupPool = (poolUrl: string, poolIndex: number) => {
    setPoolsChanged(true);
    setSelectedBackupPools((prev) => {
      // Prevent selecting the same pool for both backup slots
      if (prev.includes(poolUrl) && prev[poolIndex] !== poolUrl) {
        return prev;
      }
      const newBackupPools = [...prev];
      newBackupPools[poolIndex] = poolUrl;
      return newBackupPools;
    });
  };

  const buttonText = `Assign to ${numberOfMiners} miner${numberOfMiners === 1 ? "" : "s"}`;

  return (
    <Modal
      className="visible"
      title="Assign pools"
      showHeader
      divider={false}
      buttonSize={sizes.base}
      buttons={[
        {
          text: buttonText,
          variant: variants.primary,
        },
      ]}
      onDismiss={() => onDismiss(poolsChanged)}
      size="fullscreen"
      bodyClassName="px-60 py-14"
    >
      <div className="flex flex-col gap-6">
        {/* Page header */}
        <div className="flex flex-col gap-1">
          <h1 className="text-heading-300 text-text-primary">
            Assign pools to miners
          </h1>
          <p className="text-body-300 text-text-secondary">
            Your hashrate will contribute to your default mining pool. Add
            backup pools in case your default pool fails. Worker names are
            automatically assigned based on the miner name defined in Fleet.
          </p>
        </div>

        {/* Cards container */}
        <div className="flex flex-col gap-4">
          {/* Default pool - full width */}
          <PoolsList
            title="Default pool"
            subtitle=""
            availablePools={availablePools}
            onSelect={handleSelectDefaultPool}
            createNewLabel="Add pool"
          />

          {/* Backup pools - side by side */}
          <div className="flex gap-4">
            {[0, 1].map((index) => (
              <div key={index} className="flex-1">
                <PoolsList
                  title={`Backup pool #${index + 1}`}
                  subtitle="Optional"
                  availablePools={availablePools}
                  onSelect={(poolUrl) => handleSelectBackupPool(poolUrl, index)}
                  createNewLabel="Add pool"
                  poolNumber={index + 1}
                />
              </div>
            ))}
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default PoolsModal;
