import { useState } from "react";
import { action } from "storybook/actions";
import FoundMinersModal from "./FoundMinersModal";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import type { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

export default {
  title: "Proto Fleet/Onboarding/FoundMinersModal",
  component: FoundMinersModal,
};

const mockMiners = [
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "miner-001",
    model: "S19 Pro",
    ipAddress: "192.168.1.10",
    macAddress: "AA:BB:CC:DD:EE:01",
  },
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "miner-002",
    model: "S19 Pro",
    ipAddress: "192.168.1.11",
    macAddress: "AA:BB:CC:DD:EE:02",
  },
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "miner-003",
    model: "S19j Pro",
    ipAddress: "192.168.1.12",
    macAddress: "AA:BB:CC:DD:EE:03",
  },
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "miner-004",
    model: "S19j Pro",
    ipAddress: "192.168.1.13",
    macAddress: "AA:BB:CC:DD:EE:04",
  },
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "miner-005",
    model: "S19 XP",
    ipAddress: "192.168.1.14",
    macAddress: "AA:BB:CC:DD:EE:05",
  },
] as Device[];

export const Default = () => {
  const [open, setOpen] = useState(true);
  const [deselected, setDeselected] = useState<string[]>(["miner-004"]);

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
        deselectedMinerIds={new Set(deselected)}
        setDeselectedMiners={setDeselected}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};

// Mixed containers + rigs: header splits the total to match the inline
// FoundMiners summary ("N containers and M miners found on your network").
const mixedMiners = [
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "cu-001",
    model: "CU1",
    deviceClass: DeviceClass.CONTAINER,
    ipAddress: "192.168.1.30",
    macAddress: "AA:BB:CC:DD:EE:30",
  },
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "cu-002",
    model: "cu-200",
    deviceClass: DeviceClass.CONTAINER,
    ipAddress: "192.168.1.31",
    macAddress: "AA:BB:CC:DD:EE:31",
  },
  {
    $typeName: "pairing.v1.Device" as const,
    fleetNodeId: 0n,
    deviceIdentifier: "miner-101",
    model: "S21 Pro",
    ipAddress: "192.168.1.40",
    macAddress: "AA:BB:CC:DD:EE:40",
  },
] as Device[];

export const MixedContainersAndRigs = () => {
  const [open, setOpen] = useState(true);
  const [deselected, setDeselected] = useState<string[]>([]);

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
        miners={mixedMiners}
        deselectedMinerIds={new Set(deselected)}
        setDeselectedMiners={setDeselected}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};
