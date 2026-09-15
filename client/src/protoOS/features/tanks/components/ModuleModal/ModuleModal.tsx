import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useShallow } from "zustand/react/shallow";

import { useHashboardStatus } from "@/protoOS/api/hooks/useHashboardStatus";
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
}: {
  module: ModuleNode;
  tank: TankNode;
  topology: ContainerTopology;
}) => {
  const bodyRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const dialog = bodyRef.current?.closest<HTMLElement>('[role="dialog"]');
    if (!dialog) return;
    const previous = document.activeElement;
    dialog.querySelector<HTMLButtonElement>("button")?.focus({ preventScroll: true });
    const trapFocus = (event: KeyboardEvent) => {
      if (event.key !== "Tab") return;
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
  const modulePath = `${tankPath}/modules/${moduleRouteValue(module, modules.findIndex((item) => item.id === module.id) + 1)}`;

  return (
    <Modal
      size="fullscreen"
      title="Modules"
      dialogLabel="Modules"
      onDismiss={() => navigate(tankPath)}
      buttons={[{ text: "Done", variant: "primary" }]}
      bodyClassName="text-text-primary"
    >
      <div ref={bodyRef} className="mx-auto flex max-w-[1600px] flex-col gap-7 px-0 py-6 tablet:px-4 laptop:px-10">
        <PopoverProvider>
          <InlineSelector
            placement="right"
            currentId={module.id}
            items={modules.map((item, index) => ({
              id: item.id,
              name: `Module ${item.location?.moduleSlot ?? index + 1}`,
            }))}
            onSelect={(id) => {
              const index = modules.findIndex((item) => item.id === id);
              if (index >= 0)
                navigate(`${tankPath}/modules/${moduleRouteValue(modules[index], index + 1)}`, { replace: true });
            }}
          />
        </PopoverProvider>
        {error ? <p role="alert">Could not refresh ASIC readings. Displayed values may be out of date.</p> : null}
        <ComponentSection title="Hashboards">
          <div className="grid grid-cols-1 gap-2 laptop:grid-cols-3" aria-busy={pending}>
            {[1, 2, 3].map((slot) => {
              const board = boards.find((item) => item.location?.slot === slot);
              return board ? (
                <HashboardStatusCard
                  key={board.id}
                  identifier={board.identity.value}
                  viewTo={`${modulePath}/${encodeURIComponent(board.identity.value)}`}
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
