import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useShallow } from "zustand/react/shallow";

import ModulePowerFeed from "./ModulePowerFeed";
import ModuleVoltageSection from "./ModuleVoltageSection";

import { useHashboardStatus } from "@/protoOS/api/hooks/useHashboardStatus";
import LocationBreadcrumb from "@/protoOS/components/LocationBreadcrumb/LocationBreadcrumb";
import HealthStatusButton from "@/protoOS/components/StatusModal/HealthStatusButton";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ComponentSection from "@/protoOS/features/diagnostic/components/ComponentSection";
import EmptySlotCard from "@/protoOS/features/diagnostic/components/EmptySlotCard";
import HashboardStatusCard from "@/protoOS/features/diagnostic/components/HashboardStatusCard";
import PsuStatusCard from "@/protoOS/features/diagnostic/components/PsuStatusCard";
import { modulePoolTarget } from "@/protoOS/features/poolAssignment/targets";
import { usePoolAssignmentActions } from "@/protoOS/features/poolAssignment/usePoolAssignmentActions";
import { moduleHashboards, tankModules } from "@/protoOS/features/tanks/tankView";
import type { ContainerTopology, ModuleNode, TankNode } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { moduleRouteValue, tankRouteValue } from "@/protoOS/topologyRouteParams";
import { isMacAddress } from "@/protoOS/utils/macAddress";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import ActionBar, { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import InlineSelector from "@/shared/components/InlineSelector";
import Modal from "@/shared/components/Modal";
import { PopoverProvider } from "@/shared/components/Popover";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

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
  const poolActions = usePoolAssignmentActions();
  const boards = moduleHashboards(module, topology);
  const poolTarget = modulePoolTarget(topology, [module.id]);
  const [boardSelection, setBoardSelection] = useState<{ moduleId: string; macs: string[] }>({
    moduleId: module.id,
    macs: [],
  });
  const slots = [1, 2, 3].map((slot) => {
    const board = boards.find((item) => item.location?.slot === slot);
    const mac =
      board?.identity.kind === "mac" && isMacAddress(board.identity.value)
        ? board.identity.value.toLowerCase()
        : undefined;
    return { slot, board, mac };
  });
  const visibleMacs = new Set(slots.flatMap(({ mac }) => (mac ? [mac] : [])));
  const selectedBoards =
    boardSelection.moduleId === module.id ? boardSelection.macs.filter((mac) => visibleMacs.has(mac)) : [];
  const selectBoards = (macs: string[]) => setBoardSelection({ moduleId: module.id, macs });
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
      const controls = [
        ...dialog.querySelectorAll<HTMLElement>("button, input:not([disabled])"),
        ...document.querySelectorAll<HTMLElement>(
          "[data-inline-selector-menu] button, [data-testid$='-popover'] button",
        ),
      ].filter(
        (control) => !(control instanceof HTMLButtonElement && control.disabled) && control.getClientRects().length > 0,
      );
      if (!controls.length) return;
      const index = controls.findIndex((control) => control === document.activeElement);
      if (index < 0 || (event.shiftKey && index === 0) || (!event.shiftKey && index === controls.length - 1)) {
        event.preventDefault();
        controls[event.shiftKey ? controls.length - 1 : 0].focus();
      }
    };
    document.addEventListener("keydown", trapFocus);
    return () => {
      document.removeEventListener("keydown", trapFocus);
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true });
    };
  }, []);
  const navigate = useNavigate();
  // Leaving the module mid-write would drop the write's outcome, so the ways out wait for it.
  const [voltageWriting, setVoltageWriting] = useState(false);
  const { search } = useLocation();
  const { minerRoot } = useMinerHosting();
  const modules = tankModules(tank, topology);
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
  const showActionBar = poolActions.available && selectedBoards.length > 0;
  const footerInert = poolActions.open || voltageWriting;

  return (
    <Modal
      size="fullscreen"
      title="Modules"
      dialogLabel="Modules"
      onDismiss={voltageWriting ? undefined : (onClose ?? (() => navigate(tankPath)))}
      buttons={[{ text: "Done", variant: "primary", disabled: voltageWriting }]}
      bodyClassName="text-text-primary"
      className="overflow-y-auto"
      fixedFooter={
        showActionBar ? (
          <div inert={footerInert} className={footerInert ? "invisible" : undefined}>
            <ActionBar
              selectedItems={selectedBoards}
              itemNoun={{ singular: "hashboard", plural: "hashboards" }}
              onClose={() => selectBoards([])}
              selectionControls={
                <ActionBarSelectionControls
                  onSelectAll={visibleMacs.size > 1 ? () => selectBoards([...visibleMacs]) : undefined}
                  onSelectNone={() => selectBoards([])}
                />
              }
              renderActions={() => (
                <RowActionsMenu
                  actions={poolActions.action({ macs: selectedBoards, modules: poolTarget?.modules })}
                  ariaLabel="Actions for selected hashboards"
                  triggerLabel="Actions"
                  triggerVariant="secondary"
                  triggerClassName="bg-grayscale-white-10! text-grayscale-white-90!"
                  triggerSuffixIcon={<ChevronDown width={iconSizes.xSmall} />}
                  testIdPrefix="selected-hashboard-actions"
                />
              )}
            />
          </div>
        ) : undefined
      }
    >
      {poolActions.dialog}
      <div
        inert={poolActions.open}
        ref={bodyRef}
        className="mx-auto flex max-w-[1600px] flex-col gap-7 px-0 py-6 tablet:px-4 laptop:px-10"
      >
        <div className="flex items-center gap-2">
          <LocationBreadcrumb topology={topology} moduleId={module.id} />
          <div className="shrink-0">
            <PopoverProvider>
              <InlineSelector
                placement="right"
                headingSize="compact"
                currentId={module.id}
                disabled={voltageWriting}
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
        <div className="flex">
          <HealthStatusButton key={module.id} scope={{ kind: "module", id: module.id }} />
        </div>
        {voltageWriting ? (
          <p role="status" className="text-200 text-text-primary-70">
            Changing the module voltage. You can leave this module once the controller answers.
          </p>
        ) : null}
        {error ? <p role="alert">Could not refresh ASIC readings. Displayed values may be out of date.</p> : null}
        <ComponentSection title="Hashboards">
          <div className="grid grid-cols-1 gap-2 laptop:grid-cols-3" aria-busy={pending}>
            {slots.map(({ slot, board, mac }) => {
              return board ? (
                <HashboardStatusCard
                  key={board.id}
                  identifier={board.identity.value}
                  selection={
                    poolActions.available && mac
                      ? {
                          checked: selectedBoards.includes(mac),
                          disabled: voltageWriting,
                          onChange: (checked) =>
                            selectBoards(
                              checked
                                ? [...selectedBoards, mac]
                                : selectedBoards.filter((selected) => selected !== mac),
                            ),
                        }
                      : undefined
                  }
                  actions={voltageWriting || !mac ? undefined : poolActions.action({ macs: [mac] })}
                  viewTo={`${modulePath}/${encodeURIComponent(board.identity.value)}${search}`}
                  viewDisabled={voltageWriting}
                />
              ) : (
                <EmptySlotCard key={slot} type="hashboard" position={slot} title={`Board ${slot}`} />
              );
            })}
          </div>
        </ComponentSection>
        <ModuleVoltageSection key={module.id} module={module} onWritingChange={setVoltageWriting} />
        <ComponentSection title="Other hardware">
          {module.psuIds.length ? (
            module.psuIds.map((id) => <PsuStatusCard key={id} componentId={id} />)
          ) : (
            <p role="status">No PSU data available.</p>
          )}
          <ModulePowerFeed key={module.id} module={module} tank={tank} />
        </ComponentSection>
      </div>
    </Modal>
  );
};

export default ModuleModal;
