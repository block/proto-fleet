import { create } from "@bufbuild/protobuf";
import FoundMiners from "./FoundMiners";
import { AuthenticationMethod } from "@/protoFleet/api/generated/capabilities/v1/capabilities_pb";
import { DeviceSchema } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";

export default {
  title: "Proto Fleet/Onboarding/FoundMiners",
  component: FoundMiners,
};

function device(
  deviceIdentifier: string,
  model: string,
  manufacturer: string,
  ipAddress: string,
  supportedMethods: AuthenticationMethod[] = [],
) {
  return create(DeviceSchema, {
    deviceIdentifier,
    model,
    manufacturer,
    ipAddress,
    capabilities: { authentication: { supportedMethods } },
  });
}

// Mixed discovery: two container modules (model "CU…") plus rigs from two vendors.
// Header splits the total ("2 containers and 3 miners found on your network") and
// each group pluralizes by entity ("2 containers", "2 miners", "1 miner").
const mixedMiners = [
  device("cu-1", "CU1", "Proto", "192.168.1.30", [AuthenticationMethod.BASIC]),
  device("cu-2", "CU1", "Proto", "192.168.1.31", [AuthenticationMethod.BASIC]),
  device("s21-1", "Antminer S21", "Bitmain", "192.168.1.20"),
  device("s21-2", "Antminer S21", "Bitmain", "192.168.1.21"),
  device("rig-1", "Proto Rig", "Proto", "192.168.1.10", [AuthenticationMethod.BASIC]),
];

export const MixedContainersAndRigs = () => (
  <div className="p-6">
    <FoundMiners miners={mixedMiners} deselectedMiners={[]} className="max-w-3xl" />
  </div>
);

// Containers only: the miner bucket is omitted from the header.
const containersOnly = [
  device("cu-1", "CU1", "Proto", "192.168.1.30", [AuthenticationMethod.BASIC]),
  device("cu-2", "CU1", "Proto", "192.168.1.31", [AuthenticationMethod.BASIC]),
];

export const ContainersOnly = () => (
  <div className="p-6">
    <FoundMiners miners={containersOnly} deselectedMiners={[]} className="max-w-3xl" />
  </div>
);

// Rigs only: preserves the original "N miners found on your network" wording.
const rigsOnly = [
  device("s21-1", "Antminer S21", "Bitmain", "192.168.1.20"),
  device("s21-2", "Antminer S21", "Bitmain", "192.168.1.21"),
  device("rig-1", "Proto Rig", "Proto", "192.168.1.10", [AuthenticationMethod.BASIC]),
];

export const RigsOnly = () => (
  <div className="p-6">
    <FoundMiners miners={rigsOnly} deselectedMiners={[]} className="max-w-3xl" />
  </div>
);

// Actively scanning: shows the skeleton rows and the "…found so far" title.
export const Scanning = () => (
  <div className="p-6">
    <FoundMiners miners={mixedMiners} deselectedMiners={[]} isScanning className="max-w-3xl" />
  </div>
);
