import { MiningPool } from "../types";
import MiningPools from "@/shared/assets/icons/MiningPools";
import Button from "@/shared/components/Button";
import { sizes, variants } from "@/shared/components/Button";
import SlotNumber from "@/shared/components/SlotNumber/SlotNumber";

interface MiningPoolsListProps {
  title: string;
  subtitle: string;
  availablePools: MiningPool[];
  onSelect: (poolUrl: string) => void;
  createNewLabel: string;
  poolNumber?: number;
}

const PoolsList = ({
  title,
  subtitle,
  availablePools,
  onSelect,
  createNewLabel,
  poolNumber,
}: MiningPoolsListProps) => {
  return (
    <div className="flex flex-col rounded-xl border border-border-10 p-4">
      {/* Header */}
      <div className="mb-4 flex flex-col gap-3">
        {/* Icon */}
        <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-surface-5">
          {poolNumber !== undefined ? (
            <SlotNumber number={poolNumber} />
          ) : (
            <MiningPools className="h-5 w-5" />
          )}
        </div>

        {/* Title */}
        <div className="flex-1">
          <h3 className="text-heading-300 text-text-primary">{title}</h3>
          {subtitle ? (
            <p className="text-body-300 text-text-secondary mt-1">{subtitle}</p>
          ) : null}
        </div>
      </div>

      {/* Button */}
      <div className="flex justify-end">
        <Button
          text={createNewLabel}
          variant={variants.secondary}
          size={sizes.base}
          onClick={() => {
            // TODO: Show pool selection/creation dialog
            if (availablePools.length > 0) {
              onSelect(availablePools[0].poolUrl);
            }
          }}
        />
      </div>
    </div>
  );
};

export default PoolsList;
