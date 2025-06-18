import { useMemo } from "react";
import { getHashboardColor } from "../utility";
import { HashboardLocationStore } from "./KpiTooltip";
import { HashboardIndicator } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";

interface KpiTooltipItemProps {
  bayDivider: boolean;
  serial: string;
  currentPartial: number;
  totalPartials: number;
  value?: string | number;
  units?: string;
  hashboardLocationStore: HashboardLocationStore;
}

const KpiTooltipItem = ({
  serial,
  bayDivider,
  currentPartial,
  totalPartials,
  value,
  units,
  hashboardLocationStore,
}: KpiTooltipItemProps) => {
  const { getSlotByHbSn, getBayByHbSn, getBayCount, getBaySlotIndexByHbSn } =
    hashboardLocationStore;

  const color = useMemo(() => {
    return getHashboardColor(
      getSlotByHbSn(serial) ?? 1,
      getBayByHbSn(serial) ?? 1,
      getBaySlotIndexByHbSn(serial),
      getBayCount(),
    );
  }, [serial, getBayByHbSn, getSlotByHbSn, getBaySlotIndexByHbSn, getBayCount]);

  if (!value) return null;

  return (
    <>
      {bayDivider && (
        <div className="mb-2 px-6">
          <Divider />
        </div>
      )}
      <div className="-mt-2 flex items-center space-x-3 px-6 py-2">
        <div
          className="flex h-5 w-5 items-center justify-center text-emphasis-200"
          style={{
            color: `var(${color.text})`,
          }}
        >
          <div
            className="absolute h-5 w-5 rounded-3xl opacity-20"
            style={{
              backgroundColor: `var(${color.line})`,
            }}
          />
          {getSlotByHbSn(serial) ?? ""}
        </div>
        <HashboardIndicator
          activeHashboard={currentPartial}
          totalHashboards={totalPartials}
          color={color.line}
        />
        <div className="grow text-end text-300 text-text-primary">
          {value} {units && <span>{units}</span>}
        </div>
      </div>
    </>
  );
};

export default KpiTooltipItem;
