import { useState } from "react";
import { action } from "storybook/actions";
import FoundMinersModal from "./FoundMinersModal";
import type { MinerWithSelected } from "./types";

export default {
  title: "Proto Fleet/Onboarding/FoundMinersModal",
  component: FoundMinersModal,
};

const mockMiners = [
  {
    $typeName: "pairing.v1.Device" as const,
    deviceIdentifier: "miner-001",
    model: "S19 Pro",
    ipAddress: "192.168.1.10",
    macAddress: "AA:BB:CC:DD:EE:01",
    selected: true,
  },
  {
    $typeName: "pairing.v1.Device" as const,
    deviceIdentifier: "miner-002",
    model: "S19 Pro",
    ipAddress: "192.168.1.11",
    macAddress: "AA:BB:CC:DD:EE:02",
    selected: true,
  },
  {
    $typeName: "pairing.v1.Device" as const,
    deviceIdentifier: "miner-003",
    model: "S19j Pro",
    ipAddress: "192.168.1.12",
    macAddress: "AA:BB:CC:DD:EE:03",
    selected: true,
  },
  {
    $typeName: "pairing.v1.Device" as const,
    deviceIdentifier: "miner-004",
    model: "S19j Pro",
    ipAddress: "192.168.1.13",
    macAddress: "AA:BB:CC:DD:EE:04",
    selected: false,
  },
  {
    $typeName: "pairing.v1.Device" as const,
    deviceIdentifier: "miner-005",
    model: "S19 XP",
    ipAddress: "192.168.1.14",
    macAddress: "AA:BB:CC:DD:EE:05",
    selected: true,
  },
] as MinerWithSelected[];

export const Default = () => {
  const [open, setOpen] = useState(true);

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <FoundMinersModal
        open={open}
        miners={mockMiners}
        models={["S19 Pro", "S19j Pro", "S19 XP"]}
        setDeselectedMiners={(deselected) => action("setDeselectedMiners")(deselected)}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};
