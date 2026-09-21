import { useEffect, useRef } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useShallow } from "zustand/react/shallow";

import { useHashboardStatus } from "@/protoOS/api/hooks/useHashboardStatus";
import LocationBreadcrumb from "@/protoOS/components/LocationBreadcrumb/LocationBreadcrumb";
import HealthStatusButton from "@/protoOS/components/StatusModal/HealthStatusButton";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ComponentSection from "@/protoOS/features/diagnostic/components/ComponentSection";
import EmptySlotCard from "@/protoOS/features/diagnostic/components/EmptySlotCard";
import HashboardStatusCard from "@/protoOS/features/diagnostic/components/HashboardStatusCard";
import PsuStatusCard from "@/protoOS/features/diagnostic/components/PsuStatusCard";
import { moduleHashboards, tankModules } from "@/protoOS/features/tanks/tankView";
import type { ContainerTopology, ModuleNode, TankNode } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { moduleRouteValue, tankRouteValue } from "@/protoOS/topologyRouteParams";
import InlineSelector from "@/shared/components/InlineSelector";
import Modal from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";

const ModuleModal = ({
  module,
  tank,
  topology,
  onClose,
  modulePath: getModulePath,
  moduleLabel,
}: {
  module: ModuleNode;
  tank: TankNode;
  topology: ContainerTopology;
  onClose?: () => void;
  modulePath?: (module: ModuleNode) => string;
  moduleLabel?: (module: ModuleNode) => string;
}) => {
  const bodyRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const dialog = bodyRef.current?.closest<HTMLElement>('[role="dialog"]');
    if (!dialog) return;
    const previous = document.activeElement;
    dialog.querySelector<HTMLButtonElement>("button")?.focus({ preventScroll: true });
    const trapFocus = (event: KeyboardEvent) => {
      if (event.key !== "Tab" || [...document.querySelectorAll('[role="dialog"]')].slice(-1)[0] !== dialog) return;
      // The picker uses a portal; include its visible buttons in this modal's
      // tab order while keeping the tank page behind the modal out of it.
      const buttons = [
        ...dialog.querySelectorAll<HTMLButtonElement>("button"),
        ...document.querySelectorAll<HTMLButtonElement>("[data-inline-selector-menu] button"),
      ].filter((button) => !button.disabled && button.getClientRects().length > 0);
      if (!buttons.length) return;
      const index = buttons.findIndex((button) => button === document.activeElement);
      if (index < 0 || (event.shiftKey && index === 0) || (!event.shiftKey && index === buttons.length - 1)) {
        event.preventDefault();
        buttons[event.shiftKey ? buttons.length - 1 : 0].focus();
      }
    };
    document.addEventListener("keydown", trapFocus);
    return () => {
      document.removeEventListener("keydown", trapFocus);
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true });
    };
  }, []);
  const navigate = useNavigate();
  const { search } = useLocation();
  const { minerRoot } = useMinerHosting();
  const modules = tankModules(tank, topology);
  const boards = moduleHashboards(module, topology);
  const serials = useMinerStore(
    useShallow((state) =>
      module.hashboardIds.flatMap((id) => {
        const board = state.hardware.topology?.hashboards.get(id);
        return board ? [board.identity.value] : [];
      }),
    ),
  );
  const { error, pending } = useHashboardStatus({ hashboardSerialNumbers: serials, poll: true });
  const tankPath = `${minerRoot}/tanks/${tankRouteValue(tank)}`;
  const pathForModule = (item: ModuleNode) =>
    getModulePath?.(item) ??
    `${tankPath}/modules/${moduleRouteValue(item, modules.findIndex((candidate) => candidate.id === item.id) + 1)}`;
  const modulePath = pathForModule(module);

  return (
    <Modal
      size="fullscreen"
      title="Modules"
      dialogLabel="Modules"
      onDismiss={onClose ?? (() => navigate(tankPath))}
      buttons={[{ text: "Done", variant: "primary" }]}
      bodyClassName="text-text-primary"
    >
      <div ref={bodyRef} className="mx-auto flex max-w-[1600px] flex-col gap-7 px-0 py-6 tablet:px-4 laptop:px-10">
        <div className="flex items-center gap-2">
          <LocationBreadcrumb topology={topology} moduleId={module.id} />
          <div className="shrink-0">
            <PopoverProvider>
              <InlineSelector
                placement="right"
                headingSize="compact"
                currentId={module.id}
                items={modules.map((item, index) => ({
                  id: item.id,
                  name: moduleLabel?.(item) ?? `Module ${item.location?.moduleSlot ?? index + 1}`,
                }))}
                onSelect={(id) => {
                  const index = modules.findIndex((item) => item.id === id);
                  if (index >= 0) navigate({ pathname: pathForModule(modules[index]), search }, { replace: true });
                }}
              />
            </PopoverProvider>
          </div>
        </div>
        <div className="flex justify-start">
          <HealthStatusButton key={module.id} scope={{ kind: "module", id: module.id }} />
        </div>
        {error ? <p role="alert">Could not refresh ASIC readings. Displayed values may be out of date.</p> : null}
        <ComponentSection title="Hashboards">
          <div className="grid grid-cols-1 gap-2 laptop:grid-cols-3" aria-busy={pending}>
            {[1, 2, 3].map((slot) => {
              const board = boards.find((item) => item.location?.slot === slot);
              return board ? (
                <HashboardStatusCard
                  key={board.id}
                  identifier={board.identity.value}
                  viewTo={`${modulePath}/${encodeURIComponent(board.identity.value)}${search}`}
                />
              ) : (
                <EmptySlotCard key={slot} type="hashboard" position={slot} title={`Board ${slot}`} />
              );
            })}
          </div>
        </ComponentSection>
        <ComponentSection title="Other hardware">
          {module.psuIds.length ? (
            module.psuIds.map((id) => <PsuStatusCard key={id} componentId={id} />)
          ) : (
            <p role="status">No PSU data available.</p>
          )}
        </ComponentSection>
      </div>
    </Modal>
  );
};

export default ModuleModal;
