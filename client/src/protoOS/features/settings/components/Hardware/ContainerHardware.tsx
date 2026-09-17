import { useState } from "react";
import { containerInventory } from "./containerInventory";
import { useHardware } from "@/protoOS/api";
import { useSystemInfo } from "@/protoOS/store";
import type { HardwareState, SystemInfoData } from "@/protoOS/store/contract";
import {
  ControlBoard,
  Fan,
  Fleet,
  Hashboard,
  Immersion,
  LightningAlt,
  Notification,
  Power,
  Reboot,
  Slider,
  Speedometer,
  ThemeLight,
  Thermometer,
} from "@/shared/assets/icons";
import Modal from "@/shared/components/Modal";

const icons: Record<string, typeof Immersion> = {
  Tanks: Immersion,
  Modules: Fleet,
  PSUs: LightningAlt,
  Hashboards: Hashboard,
  Controllers: ControlBoard,
  CDU: Speedometer,
  "Coolant pumps": Reboot,
  "Dry cooler fans": Fan,
  PDUs: Power,
  "A/C": Thermometer,
  Lights: ThemeLight,
  "Control cabinet": Slider,
  Alarm: Notification,
};

export const ContainerHardwareView = ({
  hardware,
  systemInfo,
}: {
  hardware?: HardwareState;
  systemInfo?: SystemInfoData;
}) => {
  const [selected, setSelected] = useState<string>();
  const groups = containerInventory(hardware, systemInfo);
  const active = groups.find((group) => group.name === selected);

  return (
    <>
      <h2 className="mb-8 text-heading-300">Hardware</h2>
      <div className="grid grid-cols-2 gap-1 laptop:grid-cols-4">
        {groups.map((group) => {
          const Icon = icons[group.name];
          const count = group.items ? `${group.items.length} ${group.items.length === 1 ? "unit" : "units"}` : "—";
          return (
            <button
              key={group.name}
              type="button"
              aria-label={`${group.name} ${count}`}
              aria-haspopup="dialog"
              disabled={!group.items?.length}
              onClick={() => setSelected(group.name)}
              className="flex min-h-40 flex-col items-start rounded-xl bg-surface-5 p-6 text-left focus-visible:outline-2 focus-visible:outline-core-accent-fill enabled:cursor-pointer enabled:hover:bg-surface-10"
            >
              <Icon />
              <span className="mt-10 text-emphasis-300">{group.name}</span>
              <span className="mt-1 text-200 text-text-primary-70">{count}</span>
            </button>
          );
        })}
      </div>
      {active?.items?.length ? (
        <Modal
          title={active.name}
          dialogLabel={active.name}
          onDismiss={() => setSelected(undefined)}
          divider={false}
          manageFocus
        >
          <div className="overflow-x-auto">
            <table className="w-full text-left text-200">
              <thead>
                <tr className="border-b border-border-5">
                  {["Label", "Model", "Serial number", "Firmware"].map((heading) => (
                    <th key={heading} scope="col" className="px-2 py-3 font-medium first:pl-0">
                      {heading}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {active.items.map((item) => (
                  <tr key={item.id} className="border-b border-border-5">
                    {[item.label, item.model, item.serialNumber, item.firmware].map((value, index) => (
                      <td key={index} className="px-2 py-3 break-words first:pl-0">
                        {value || "—"}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Modal>
      ) : null}
    </>
  );
};

const ContainerHardware = () => {
  const { data, pending, error } = useHardware();
  const systemInfo = useSystemInfo();
  return <ContainerHardwareView hardware={error || pending ? undefined : data} systemInfo={systemInfo} />;
};

export default ContainerHardware;
