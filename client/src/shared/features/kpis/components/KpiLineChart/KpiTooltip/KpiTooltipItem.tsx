import { getHashboardColor } from "../utility";
import { HashboardLocationStore } from "./KpiTooltip";
import { HashboardIndicator } from "@/shared/assets/icons";
import { Circle } from "@/shared/assets/icons";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface KpiTooltipItemProps {
  serial: string;
  currentPartial: number;
  totalSlots: number;
  value?: string | number;
  units?: string;
  hashboardLocationStore: HashboardLocationStore;
}

const KpiTooltipItem = ({
  serial,
  currentPartial,
  totalSlots,
  value,
  units,
  hashboardLocationStore,
}: KpiTooltipItemProps) => {
  const { getSlotByHbSn } = hashboardLocationStore;
  const { isPhone } = useWindowDimensions();

  const color = useCssVariable(
    getHashboardColor(getSlotByHbSn(serial)) || "--color-bg-core-primary-5",
  );

  if (!value) return null;

  return (
    <>
      <div className="-mt-2 flex items-center justify-between space-x-3 py-2">
        <div className="inline-flex items-center gap-2">
          <Circle style={{ backgroundColor: color }} width="w-2" />
          <div className="grow text-end text-300 text-text-primary">
            {value} {units && <span>{units}</span>}
          </div>
        </div>

        {!isPhone && (
          <div className="inline-flex items-center gap-2">
            <div className="flex h-5 w-5 items-center justify-center rounded-3xl bg-core-primary-5 text-emphasis-200 text-text-primary">
              {getSlotByHbSn(serial) ?? ""}
            </div>
            <HashboardIndicator
              activeHashboardSlot={getSlotByHbSn(serial) ?? currentPartial + 1}
              totalHashboards={totalSlots}
            />
          </div>
        )}
      </div>
    </>
  );
};

export default KpiTooltipItem;
