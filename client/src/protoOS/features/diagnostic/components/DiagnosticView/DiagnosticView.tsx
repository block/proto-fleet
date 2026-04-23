import { useState } from "react";
import ComponentSection from "../ComponentSection";
import ComponentSelector from "../ComponentSelector";
import type { ComponentFilterType } from "../ComponentSelector/types";
import ControlBoardStatusCard from "../ControlBoardStatusCard";
import EmptySlotCard from "../EmptySlotCard";
import FanStatusCard from "../FanStatusCard";
import HashboardStatusCard from "../HashboardStatusCard";
import PsuStatusCard from "../PsuStatusCard";
import { TOTAL_FAN_SLOTS, TOTAL_PSU_SLOTS, useCoolingStatus, useTelemetry } from "@/protoOS/api";
import {
  useBayCount,
  useControlBoard,
  useCoolingMode,
  useFanIds,
  useHashboardSerialsByBay,
  usePsuIds,
  useSlotsPerBay,
} from "@/protoOS/store";
import { areAllFansDisconnected } from "@/protoOS/store/utils/coolingUtils";
import Immersion from "@/shared/assets/icons/Immersion";
import { ErrorBoundary } from "@/shared/components/ErrorBoundary";

interface DiagnosticViewProps {
  className?: string;
}

const NoFansEmptyState = () => (
  <div className="flex flex-col items-center justify-center rounded-3xl bg-surface-5 px-20 py-10">
    <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-lg bg-core-primary-5">
      <Immersion />
    </div>
    <div className="text-heading-200">No fans to display</div>
    <div className="text-400 text-text-primary-70">This miner is set to immersion cooling.</div>
  </div>
);

const FansGrid = ({ occupiedSlots }: { occupiedSlots: Set<number> }) => (
  <div className="grid gap-1 tablet:grid-cols-2 laptop:grid-cols-2 desktop:auto-cols-fr desktop:grid-flow-col desktop:grid-rows-2">
    {Array.from({ length: TOTAL_FAN_SLOTS }, (_, i) => {
      const slot = i + 1;
      if (occupiedSlots.has(slot)) {
        return <FanStatusCard key={slot} slot={slot} />;
      }
      return <EmptySlotCard key={`fan-empty-${slot}`} type="fan" position={slot} title={`Fan ${slot}`} />;
    })}
  </div>
);

const FansSection = () => {
  const fanIds = useFanIds();
  const coolingMode = useCoolingMode();
  const occupiedSlots = new Set(fanIds);

  // Use cooling API for reliable fan detection (hardware API has null placeholders from unimplemented fan calibration)
  const { data: coolingData } = useCoolingStatus();

  const noFansConnected = areAllFansDisconnected(coolingData?.fans);
  const isImmersionMode = coolingMode === "Off";
  const showNoFansState = noFansConnected && isImmersionMode;

  return (
    <ComponentSection title="Fans">
      {showNoFansState ? <NoFansEmptyState /> : <FansGrid occupiedSlots={occupiedSlots} />}
    </ComponentSection>
  );
};
FansSection.displayName = "FansSection";

const HashboardsSection = () => {
  const hashboardsByBay = useHashboardSerialsByBay();
  const bayCount = useBayCount();
  const slotsPerBay = useSlotsPerBay();

  const totalBays = bayCount || 3;
  const bayIndices = Array.from({ length: totalBays }, (_, i) => i + 1);

  return (
    <ComponentSection title="Hashboards">
      <div className="grid gap-1 tablet:grid-cols-2 laptop:grid-cols-2 desktop:grid-flow-col desktop:grid-cols-3 desktop:grid-rows-3">
        {bayIndices
          .map((bayIndex) => {
            const serialsInBay = hashboardsByBay[bayIndex] || Array(slotsPerBay).fill(null);

            return serialsInBay.map((serial, slotIndex) => {
              if (serial) {
                return <HashboardStatusCard key={serial} serialNumber={serial} />;
              }

              const slotNumber = (bayIndex - 1) * slotsPerBay + slotIndex + 1;
              return (
                <EmptySlotCard
                  key={`hashboard-empty-${slotNumber}`}
                  type="hashboard"
                  position={slotNumber}
                  title={`Slot ${slotNumber}`}
                />
              );
            });
          })
          .flat()}
      </div>
    </ComponentSection>
  );
};
HashboardsSection.displayName = "HashboardsSection";

const PsusSection = () => {
  const psuIds = usePsuIds();
  const occupiedSlots = new Set(psuIds);

  return (
    <ComponentSection title="PSU">
      <div className="grid gap-1 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: TOTAL_PSU_SLOTS }, (_, i) => {
          const slot = i + 1;
          if (occupiedSlots.has(slot)) {
            return <PsuStatusCard key={slot} slot={slot} />;
          }
          return <EmptySlotCard key={`psu-empty-${slot}`} type="psu" position={slot} title={`PSU ${slot}`} />;
        })}
      </div>
    </ComponentSection>
  );
};
PsusSection.displayName = "PsusSection";

const ControlBoardSection = () => {
  const controlBoard = useControlBoard();

  if (!controlBoard) return null;

  return (
    <ComponentSection title="Control Board">
      <div className="grid gap-1 md:grid-cols-2 xl:grid-cols-3">
        <ControlBoardStatusCard />
      </div>
    </ComponentSection>
  );
};
ControlBoardSection.displayName = "ControlBoardSection";

function DiagnosticView({ className }: DiagnosticViewProps) {
  useTelemetry({ level: ["hashboard", "asic", "psu"] });
  const [selectedComponent, setSelectedComponent] = useState<ComponentFilterType>("all");

  const shouldShowComponent = (component: ComponentFilterType) => {
    return selectedComponent === "all" || selectedComponent === component;
  };

  return (
    <div className={`w-full space-y-6 ${className || ""}`}>
      <div className="flex flex-col items-start gap-3 pb-6 sm:flex-row sm:items-center">
        <div className="grow text-heading-300">Diagnostics</div>
        <ComponentSelector selectedComponent={selectedComponent} onSelect={setSelectedComponent} />
      </div>

      <div className="space-y-12">
        {shouldShowComponent("fans") && (
          <ErrorBoundary>
            <FansSection />
          </ErrorBoundary>
        )}

        {shouldShowComponent("hashboards") && (
          <ErrorBoundary>
            <HashboardsSection />
          </ErrorBoundary>
        )}

        {shouldShowComponent("psus") && (
          <ErrorBoundary>
            <PsusSection />
          </ErrorBoundary>
        )}

        {shouldShowComponent("controlBoard") && (
          <ErrorBoundary>
            <ControlBoardSection />
          </ErrorBoundary>
        )}
      </div>
    </div>
  );
}

export default DiagnosticView;
