import { createElement, Fragment, type ReactNode, useEffect } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient, deviceSetClient, fleetManagementClient } from "@/protoFleet/api/clients";
import {
  CurtailmentCandidateSchema,
  CurtailmentMode,
  type FixedKwParams,
  FixedKwParamsSchema,
  PreviewCurtailmentPlanResponseSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  DeviceSetSchema,
  DeviceSetType,
  GroupInfoSchema,
  ListDeviceSetsResponseSchema,
  RackInfoSchema,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  ListMinerStateSnapshotsResponseSchema,
  MinerStateSnapshotSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { createRefCountedStoryMock } from "@/shared/stories/createRefCountedStoryMock";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };

const mutableDeviceSetClient = deviceSetClient as MutableClient<typeof deviceSetClient>;
const mutableFleetManagementClient = fleetManagementClient as MutableClient<typeof fleetManagementClient>;
const mutableCurtailmentClient = curtailmentClient as MutableClient<typeof curtailmentClient>;

const mockRacks = [
  create(DeviceSetSchema, {
    id: 101n,
    type: DeviceSetType.RACK,
    label: "Rack A1",
    deviceCount: 8,
    typeDetails: {
      case: "rackInfo",
      value: create(RackInfoSchema, {
        rows: 4,
        columns: 2,
        zone: "North Hall",
      }),
    },
  }),
  create(DeviceSetSchema, {
    id: 102n,
    type: DeviceSetType.RACK,
    label: "Rack B4",
    deviceCount: 12,
    typeDetails: {
      case: "rackInfo",
      value: create(RackInfoSchema, {
        rows: 6,
        columns: 2,
        zone: "South Hall",
      }),
    },
  }),
];

const mockGroups = [
  create(DeviceSetSchema, {
    id: 201n,
    type: DeviceSetType.GROUP,
    label: "High priority",
    deviceCount: 10,
    typeDetails: {
      case: "groupInfo",
      value: create(GroupInfoSchema, {}),
    },
  }),
  create(DeviceSetSchema, {
    id: 202n,
    type: DeviceSetType.GROUP,
    label: "Low efficiency",
    deviceCount: 6,
    typeDetails: {
      case: "groupInfo",
      value: create(GroupInfoSchema, {}),
    },
  }),
];

const mockMiners = [
  create(MinerStateSnapshotSchema, {
    deviceIdentifier: "miner-9",
    name: "Miner 9",
    model: "S21 Pro",
    ipAddress: "10.0.0.9",
    placement: {
      rack: { id: 101n, label: "Rack A1" },
      groups: [{ id: 201n, label: "High priority" }],
    },
  }),
  create(MinerStateSnapshotSchema, {
    deviceIdentifier: "miner-14",
    name: "Miner 14",
    model: "S19 XP",
    ipAddress: "10.0.0.14",
    placement: {
      rack: { id: 102n, label: "Rack B4" },
      groups: [{ id: 202n, label: "Low efficiency" }],
    },
  }),
  create(MinerStateSnapshotSchema, {
    deviceIdentifier: "miner-22",
    name: "Miner 22",
    model: "S21 Pro",
    ipAddress: "10.0.0.22",
    placement: { rack: { id: 102n, label: "Rack B4" } },
  }),
];

const mockMinerModels = Array.from(new Set(mockMiners.map((miner) => miner.model)));
const mockCurtailmentCandidates = mockMiners.map((miner, index) =>
  create(CurtailmentCandidateSchema, {
    deviceIdentifier: miner.deviceIdentifier,
    currentPowerW: 2800 + index * 400,
    efficiencyJh: 18 + index,
    reasonSelected: "Least efficient available miner",
  }),
);

export function MockedMinerSelectionApis({ children }: { children: ReactNode }) {
  useEffect(() => {
    return installMockedMinerSelectionApis();
  }, []);

  return createElement(Fragment, null, children);
}

export function withMockedMinerSelectionApis(Story: () => ReactNode) {
  return createElement(MockedMinerSelectionApis, null, createElement(Story));
}

const installMockedMinerSelectionApis = createRefCountedStoryMock(() => {
  const originalListDeviceSets = mutableDeviceSetClient.listDeviceSets;
  const originalListMinerStateSnapshots = mutableFleetManagementClient.listMinerStateSnapshots;
  const originalPreviewCurtailmentPlan = mutableCurtailmentClient.previewCurtailmentPlan;

  mutableDeviceSetClient.listDeviceSets = async (request) => {
    const deviceSets = request.type === DeviceSetType.RACK ? mockRacks : mockGroups;

    return create(ListDeviceSetsResponseSchema, {
      deviceSets,
      nextPageToken: "",
      totalCount: deviceSets.length,
    });
  };

  mutableFleetManagementClient.listMinerStateSnapshots = async () =>
    create(ListMinerStateSnapshotsResponseSchema, {
      miners: mockMiners,
      cursor: "",
      totalMiners: mockMiners.length,
      models: mockMinerModels,
    });

  mutableCurtailmentClient.previewCurtailmentPlan = async (request) => {
    const fixedKw =
      request.modeParams?.case === "fixedKw" ? (request.modeParams.value as Partial<FixedKwParams>) : undefined;
    const fixedTargetKw = typeof fixedKw?.targetKw === "number" ? fixedKw.targetKw : undefined;
    const fixedToleranceKw = typeof fixedKw?.toleranceKw === "number" ? fixedKw.toleranceKw : undefined;
    const estimatedReductionKw =
      request.mode === CurtailmentMode.FIXED_KW && fixedTargetKw
        ? fixedTargetKw
        : mockCurtailmentCandidates.length * 3.2;

    return create(PreviewCurtailmentPlanResponseSchema, {
      candidates: mockCurtailmentCandidates,
      estimatedReductionKw,
      estimatedRemainingPowerKw: 12.5,
      mode: request.mode,
      modeParams: fixedKw
        ? {
            case: "fixedKw",
            value: create(FixedKwParamsSchema, {
              targetKw: fixedTargetKw,
              toleranceKw: fixedToleranceKw,
            }),
          }
        : { case: undefined },
      skippedCandidates: [],
    });
  };

  return () => {
    mutableDeviceSetClient.listDeviceSets = originalListDeviceSets;
    mutableFleetManagementClient.listMinerStateSnapshots = originalListMinerStateSnapshots;
    mutableCurtailmentClient.previewCurtailmentPlan = originalPreviewCurtailmentPlan;
  };
});
