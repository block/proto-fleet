import { useState } from "react";
import ComponentSection from "../ComponentSection";
import ComponentSelector from "../ComponentSelector";
import type { ComponentFilterType } from "../ComponentSelector/types";
import ControlBoardStatusCard from "../ControlBoardStatusCard";
import EmptySlotCard from "../EmptySlotCard";
import FanStatusCard from "../FanStatusCard";
import HashboardStatusCard from "../HashboardStatusCard";
import PsuStatusCard from "../PsuStatusCard";
import { useTelemetry } from "@/protoOS/api";
// import { useErrors, useTelemetry } from "@/protoOS/api";
// import { transformNotificationErrors } from "@/protoOS/features/diagnostic/utils/componentErrorUtils";
import {
  useControlBoard,
  useFanIds,
  useHashboardSerials,
  useHashboardsHardware,
  useMinerFans,
  useMinerPsus,
  usePsuIds,
} from "@/protoOS/store";
// import ComponentStatusModal from "@/shared/components/ComponentStatusModal";
import { ErrorBoundary } from "@/shared/components/ErrorBoundary";

interface DiagnosticViewProps {
  className?: string;
}

const FansSection = () => {
  const fanIds = useFanIds();
  const fans = useMinerFans();

  return (
    <ComponentSection title="Fans">
      <div className="grid gap-1 sm:grid-cols-2 lg:auto-cols-fr lg:grid-flow-col lg:grid-rows-2">
        {fanIds.map((id) => (
          <FanStatusCard key={id} fanId={id} />
        ))}
        {/* Render empty slots - assuming 4 total fan slots */}
        {fans.length < 4 &&
          Array.from({ length: 4 - fans.length }, (_, i) => {
            const position = fans.length + i + 1;
            return (
              <EmptySlotCard
                key={`fan-empty-${position}`}
                type="fan"
                position={position}
                title={`Fan ${position}`}
              />
            );
          })}
      </div>
    </ComponentSection>
  );
};
FansSection.displayName = "FansSection";

const HashboardsSection = () => {
  const hashboardSerials = useHashboardSerials();
  const hashboards = useHashboardsHardware();

  return (
    <ComponentSection title="Hashboards">
      <div className="grid gap-1 md:grid-cols-2 lg:grid-cols-3">
        {hashboardSerials.map((serial) => (
          <HashboardStatusCard key={serial} serialNumber={serial} />
        ))}
        {/* Render empty slots */}
        {hashboards.length < 3 &&
          Array.from({ length: 3 - hashboards.length }, (_, i) => {
            const position = hashboards.length + i + 1;
            return (
              <EmptySlotCard
                key={`hashboard-empty-${position}`}
                type="hashboard"
                position={position}
                title={`Hashboard ${position}`}
              />
            );
          })}
      </div>
    </ComponentSection>
  );
};
HashboardsSection.displayName = "HashboardsSection";

const PsusSection = () => {
  const psuIds = usePsuIds();
  const psus = useMinerPsus();

  return (
    <ComponentSection title="PSU">
      <div className="grid gap-1 md:grid-cols-2 lg:grid-cols-3">
        {psuIds.map((id) => (
          <PsuStatusCard key={id} psuId={id} />
        ))}
        {/* Render empty slots - assuming 3 total PSU slots */}
        {psus.length < 3 &&
          Array.from({ length: 3 - psus.length }, (_, i) => {
            const position = psus.length + i + 1;
            return (
              <EmptySlotCard
                key={`psu-empty-${position}`}
                type="psu"
                position={position}
                title={`PSU ${position}`}
              />
            );
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
      <div className="grid gap-1 md:grid-cols-2 lg:grid-cols-3">
        <ControlBoardStatusCard />
      </div>
    </ComponentSection>
  );
};
ControlBoardSection.displayName = "ControlBoardSection";

function DiagnosticView({ className }: DiagnosticViewProps) {
  useTelemetry({ level: ["asic"] });

  // Fetch errors
  // const { data: errors } = useErrors();

  // Component filter state
  const [selectedComponent, setSelectedComponent] =
    useState<ComponentFilterType>("all");

  // Error modal state
  // const [showErrorModal, setShowErrorModal] = useState(false);

  // Transform errors for modal
  // const componentErrors = useMemo(() => {
  //   if (!errors || errors.length === 0) return [];
  //   return transformNotificationErrors(errors);
  // }, [errors]);

  const shouldShowComponent = (component: ComponentFilterType) => {
    return selectedComponent === "all" || selectedComponent === component;
  };

  return (
    <div className={`w-full space-y-6 ${className || ""}`}>
      {/* Component Selector */}
      <div className="flex flex-col items-start gap-3 pb-6 sm:flex-row sm:items-center">
        <div className="grow text-heading-300">Diagnostics</div>
        <ComponentSelector
          selectedComponent={selectedComponent}
          onSelect={setSelectedComponent}
        />
      </div>

      <div className="space-y-12">
        {/* Fans Section */}
        {shouldShowComponent("fans") && (
          <ErrorBoundary>
            <FansSection />
          </ErrorBoundary>
        )}

        {/* Hashboards Section */}
        {shouldShowComponent("hashboards") && (
          <ErrorBoundary>
            <HashboardsSection />
          </ErrorBoundary>
        )}

        {/* PSUs Section */}
        {shouldShowComponent("psus") && (
          <ErrorBoundary>
            <PsusSection />
          </ErrorBoundary>
        )}

        {/* Control Board Section */}
        {shouldShowComponent("controlBoard") && (
          <ErrorBoundary>
            <ControlBoardSection />
          </ErrorBoundary>
        )}
      </div>

      {/* Component Errors Modal - TODO: Update to use new ComponentStatusModal interface */}
      {/* {showErrorModal && (
        <ComponentStatusModal
          summary="Component errors"
          componentType="controlBoard"
          issues={componentErrors}
          onDismiss={() => setShowErrorModal(false)}
        />
      )} */}
    </div>
  );
}

export default DiagnosticView;
