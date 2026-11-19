import { describe, expect, it } from "vitest";

import { resolveKpiViewSelection } from "./kpiViewSelection";
import {
  asContainerId,
  asDeviceId,
  asHashboardId,
  asModuleId,
  asTankId,
  type CommandCapabilities,
  type ContainerTopology,
  ROOT_MODULE_ID,
  type Topology,
} from "@/protoOS/store/contract";

const commands: CommandCapabilities = {
  startStop: "unknown",
  powerTarget: "unknown",
  locate: "unknown",
  reboot: "unknown",
  cooling: "unknown",
  logs: "unknown",
  firmwareUpdate: "unknown",
  poolEditing: "unknown",
  passwordManagement: "unknown",
};

const containerTopology = (): ContainerTopology => {
  const containerId = asContainerId("container:ct1-01");
  const tankId = asTankId("tank:slotless");
  const moduleId = asModuleId("module:slotless");
  const hashboardId = asHashboardId("serial:HB-1");

  return {
    kind: "container",
    deviceId: asDeviceId("device:ct1-01"),
    containerId,
    container: {
      id: containerId,
      capabilities: commands,
      tankIds: [tankId],
      controlBoardIds: [],
      fanIds: [],
      plantComponentIds: [],
    },
    tanks: new Map([
      [
        tankId,
        {
          id: tankId,
          containerId,
          capabilities: commands,
          moduleIds: [moduleId],
          label: "Tank A",
        },
      ],
    ]),
    modules: new Map([
      [
        moduleId,
        {
          id: moduleId,
          profile: "ct1",
          capabilities: { commands },
          tankId,
          hashboardIds: [hashboardId],
          controlBoardIds: [],
          psuIds: [],
          fanIds: [],
        },
      ],
    ]),
    hashboards: new Map(),
  };
};

describe("resolveKpiViewSelection", () => {
  it("keeps rig routes scoped to the root module", () => {
    const topology: Topology = {
      kind: "rig",
      deviceId: asDeviceId("device:r2"),
      capabilities: commands,
      rootModuleId: ROOT_MODULE_ID,
      modules: new Map(),
      hashboards: new Map(),
    };

    expect(resolveKpiViewSelection(topology)).toEqual({
      level: "rig",
      moduleId: ROOT_MODULE_ID,
    });
  });

  it("resolves a container to the container home selection", () => {
    const topology = containerTopology();

    expect(resolveKpiViewSelection(topology)).toEqual({
      level: "container",
      containerId: topology.containerId,
    });
  });
});
