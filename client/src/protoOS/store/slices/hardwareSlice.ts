import type { StateCreator } from "zustand";

import {
  asAsicId,
  asComponentId,
  asDeviceId,
  type AsicHardware,
  type AsicId,
  type HardwareState,
  type HashboardId,
  type HashboardNode,
  ROOT_MODULE_ID,
  type Topology,
} from "../contract";
import type {
  AsicHardwareData,
  ControlBoardHardwareData,
  FanHardwareData,
  HashboardHardwareData,
  MinerHardwareData,
  PsuHardwareData,
} from "../types";
import type { MinerStore } from "../useMinerStore";
import { getFanId, getHashboardId, getPsuId } from "../utils/entityIds";

const owner = { kind: "module" as const, id: ROOT_MODULE_ID };
const unknownCommands = {
  startStop: "unknown" as const,
  powerTarget: "unknown" as const,
  locate: "unknown" as const,
  reboot: "unknown" as const,
  cooling: "unknown" as const,
  logs: "unknown" as const,
  firmwareUpdate: "unknown" as const,
  poolEditing: "unknown" as const,
  passwordManagement: "unknown" as const,
};

export interface HardwareSlice extends HardwareState {
  reset: () => void;
  replaceHardware: (hardware: HardwareState) => void;

  // Compatibility writers/selectors for existing diagnostic stories and the
  // v1 hashboard-detail endpoint. Stored values remain contract types.
  setMiner: (miner: MinerHardwareData) => void;
  getMiner: () => MinerHardwareData | null;
  setControlBoard: (controlBoard: ControlBoardHardwareData) => void;
  getControlBoard: () => ControlBoardHardwareData | null;
  setHashboards: (hashboards: HashboardHardwareData[]) => void;
  addHashboard: (hashboard: HashboardHardwareData) => void;
  getHashboard: (serial: string) => HashboardHardwareData | undefined;
  getHashboardBySlot: (slot: number) => HashboardHardwareData | undefined;
  getHashboardsByBay: (bay: number) => HashboardHardwareData[];
  getBayCount: () => number;
  getSlotByHbSn: (serial: string) => number | undefined;
  getBayByHbSn: (serial: string) => number | undefined;
  setAsics: (asics: AsicHardwareData[]) => void;
  addAsic: (asic: AsicHardwareData) => void;
  batchAddAsics: (asics: AsicHardwareData[]) => void;
  getAsic: (id: string) => AsicHardwareData | undefined;
  getAsicsByHashboard: (serial: string) => AsicHardwareData[];
  getAsicPosition: (id: string) => { row?: number; column?: number } | undefined;
  getAsicRowsByHashboard: (serial: string) => number[];
  linkAsicToHashboard: (asicId: string, serial: string) => void;
  setPsus: (psus: PsuHardwareData[]) => void;
  addPsu: (psu: PsuHardwareData) => void;
  getPsu: (id: number) => PsuHardwareData | undefined;
  getAllPsus: () => PsuHardwareData[];
  setFans: (fans: FanHardwareData[]) => void;
  addFan: (fan: FanHardwareData) => void;
  getFan: (slot: number) => FanHardwareData | undefined;
  getAllFans: () => FanHardwareData[];
}

const ensureTopology = (state: HardwareSlice) => {
  if (state.topology) return state.topology;
  state.topology = {
    kind: "directModule",
    deviceId: asDeviceId("device:legacy"),
    moduleId: ROOT_MODULE_ID,
    modules: new Map([
      [
        ROOT_MODULE_ID,
        {
          id: ROOT_MODULE_ID,
          profile: "unknown",
          capabilities: { commands: unknownCommands },
          hashboardIds: [],
          controlBoardIds: [],
          psuIds: [],
          fanIds: [],
        },
      ],
    ]),
    hashboards: new Map(),
  };
  return state.topology!;
};

const nodeForSerial = (state: HardwareSlice, serial: string) =>
  state.topology ? [...state.topology.hashboards.values()].find((node) => node.identity.value === serial) : undefined;

const hashboardView = (state: HardwareSlice, serial: string): HashboardHardwareData | undefined => {
  const node = nodeForSerial(state, serial);
  const hardware = node ? state.hashboards.get(node.id) : state.hashboards.get(getHashboardId(serial));
  if (!hardware) return undefined;
  return {
    serial: hardware.identity.value,
    slot: node?.location?.slot,
    bay: node?.location?.bay,
    port: node?.location?.port,
    board: hardware.boardModel,
    asicIds: node?.asicIds.map(String),
    apiVersion: hardware.apiVersion,
    chipId: hardware.chipId,
    miningAsic: hardware.miningAsic === "unknown" ? undefined : hardware.miningAsic,
    miningAsicCount: hardware.miningAsicCount,
    temperatureSensorCount: hardware.temperatureSensorCount,
    ecLogsPath: hardware.ecLogsPath,
    firmware: hardware.firmware
      ? {
          ...hardware.firmware,
          build:
            hardware.firmware.build === "debug" || hardware.firmware.build === "release"
              ? hardware.firmware.build
              : undefined,
        }
      : undefined,
    bootloader: hardware.bootloader
      ? {
          ...hardware.bootloader,
          build:
            hardware.bootloader.build === "debug" || hardware.bootloader.build === "release"
              ? hardware.bootloader.build
              : undefined,
        }
      : undefined,
  };
};

const upsertHashboard = (state: HardwareSlice, input: HashboardHardwareData) => {
  const id = getHashboardId(input.serial);
  const existing = state.hashboards.get(id);
  state.hashboards.set(id, {
    ref: { id, kind: "hashboard", owner },
    identity: { kind: "serial", value: input.serial },
    boardModel: input.board ?? existing?.boardModel,
    apiVersion: input.apiVersion ?? existing?.apiVersion,
    chipId: input.chipId ?? existing?.chipId,
    miningAsic: input.miningAsic ?? existing?.miningAsic,
    miningAsicCount: input.miningAsicCount ?? existing?.miningAsicCount,
    temperatureSensorCount: input.temperatureSensorCount ?? existing?.temperatureSensorCount,
    ecLogsPath: input.ecLogsPath ?? existing?.ecLogsPath,
    firmware: input.firmware ?? existing?.firmware,
    bootloader: input.bootloader ?? existing?.bootloader,
  });
  const topology = ensureTopology(state);
  const prior = topology.hashboards.get(id);
  topology.hashboards.set(id, {
    id,
    identity: { kind: "serial", value: input.serial },
    moduleId: ROOT_MODULE_ID,
    asicIds: (input.asicIds ?? prior?.asicIds ?? []).map(asAsicId),
    location: {
      slot: input.slot ?? prior?.location?.slot,
      bay: input.bay ?? prior?.location?.bay,
      port: input.port ?? prior?.location?.port,
    },
  });
  const module = topology.modules.get(ROOT_MODULE_ID);
  if (module && !module.hashboardIds.includes(id)) module.hashboardIds.push(id);
};

const asicView = (asic: AsicHardware): AsicHardwareData => ({
  id: String(asic.ref.id),
  hashboardSerial: String(asic.hashboardId).replace(/^serial:/, ""),
  index: asic.index,
  row: asic.row,
  column: asic.column,
});

const upsertAsic = (state: HardwareSlice, item: AsicHardwareData) => {
  const id = asAsicId(item.id);
  state.asics.set(id, {
    ref: { id, kind: "asic", owner },
    hashboardId: getHashboardId(item.hashboardSerial),
    index: item.index,
    row: item.row,
    column: item.column,
  });
};

const upsertPsu = (state: HardwareSlice, item: PsuHardwareData) => {
  const slot = item.slot ?? item.id;
  const id = getPsuId(slot);
  state.psus.set(id, {
    ref: { id, kind: "psu", owner },
    slot,
    serialNumber: item.serial,
    manufacturer: item.manufacturer,
    model: item.model,
    hardwareRevision: item.hwRevision,
    firmware: item.firmware
      ? {
          application: { version: item.firmware.appVersion },
          bootloader: { version: item.firmware.bootloaderVersion },
        }
      : undefined,
  });
};

const upsertFan = (state: HardwareSlice, item: FanHardwareData) => {
  const id = getFanId(item.slot);
  state.fans.set(id, { ref: { id, kind: "fan", owner }, slot: item.slot, name: item.name });
};

export const createHardwareSlice: StateCreator<MinerStore, [["zustand/immer", never]], [], HardwareSlice> = (
  set,
  get,
) => ({
  resource: { status: "idle" },
  topology: undefined,
  controlBoards: new Map(),
  hashboards: new Map(),
  asics: new Map(),
  psus: new Map(),
  fans: new Map(),

  reset: () =>
    set((state) => {
      Object.assign(state.hardware, {
        resource: { status: "idle" },
        topology: undefined,
        controlBoards: new Map(),
        hashboards: new Map(),
        asics: new Map(),
        psus: new Map(),
        fans: new Map(),
      });
    }),
  replaceHardware: (hardware) => {
    const previous = get().hardware;
    const asics = new Map(hardware.asics);
    const preservedAsicIds = new Map<HashboardId, AsicId[]>();
    previous.asics.forEach((asic, id) => {
      if (!hardware.hashboards.has(asic.hashboardId) || asics.has(id)) return;
      asics.set(id, asic);
      const ids = preservedAsicIds.get(asic.hashboardId) ?? [];
      ids.push(id);
      preservedAsicIds.set(asic.hashboardId, ids);
    });
    const topology = hardware.topology
      ? ({
          ...hardware.topology,
          hashboards: new Map(
            [...hardware.topology.hashboards].map(([id, node]) => [
              id,
              {
                ...node,
                asicIds: [...new Set([...node.asicIds, ...(preservedAsicIds.get(id) ?? [])])],
              },
            ]),
          ),
        } satisfies Topology)
      : undefined;

    set((state) => {
      Object.assign(state.hardware, { ...hardware, topology, asics });
    });
  },
  setMiner: () => undefined,
  getMiner: () => {
    const state = get().hardware;
    const serials = [...state.hashboards.values()].map((board) => board.identity.value);
    return serials.length
      ? { hashboardSerials: serials, controlBoardSerial: [...state.controlBoards.values()][0]?.serialNumber }
      : null;
  },
  setControlBoard: (input) =>
    set((state) => {
      const id = asComponentId(`${ROOT_MODULE_ID}:controlBoard:1`);
      state.hardware.controlBoards.set(id, {
        ref: { id, kind: "controlBoard", owner },
        role: "primary",
        serialNumber: input.serial,
        boardId: input.boardId,
        machineName: input.machineName,
        firmware: input.firmware,
        cpu: input.mpu
          ? {
              modelName: input.mpu.modelName,
              architecture: input.mpu.cpuArchitecture?.toString(),
              hardware: input.mpu.hardware,
              revision: input.mpu.revision,
              implementer: input.mpu.cpuImplementer,
              part: input.mpu.cpuPart,
              variant: input.mpu.cpuVariant,
            }
          : undefined,
      });
    }),
  getControlBoard: () => {
    const board = [...get().hardware.controlBoards.values()][0];
    return board
      ? {
          serial: board.serialNumber,
          boardId: board.boardId,
          machineName: board.machineName,
          firmware: board.firmware,
          mpu: board.cpu
            ? {
                modelName: board.cpu.modelName,
                cpuArchitecture: Number(board.cpu.architecture) || undefined,
                hardware: board.cpu.hardware,
                revision: board.cpu.revision,
                cpuImplementer: board.cpu.implementer,
                cpuPart: board.cpu.part,
                cpuVariant: board.cpu.variant,
              }
            : undefined,
        }
      : null;
  },
  setHashboards: (items) =>
    set((state) => {
      state.hardware.hashboards.clear();
      ensureTopology(state.hardware).hashboards.clear();
      items.forEach((item) => upsertHashboard(state.hardware, item));
    }),
  addHashboard: (item) => set((state) => upsertHashboard(state.hardware, item)),
  getHashboard: (serial) => hashboardView(get().hardware, serial),
  getHashboardBySlot: (slot) => {
    const state = get().hardware;
    const node = state.topology
      ? [...state.topology.hashboards.values()].find((item) => item.location?.slot === slot)
      : undefined;
    return node ? hashboardView(state, node.identity.value) : undefined;
  },
  getHashboardsByBay: (bay) => {
    const state = get().hardware;
    return state.topology
      ? [...state.topology.hashboards.values()]
          .filter((node) => node.location?.bay === bay)
          .map((node) => hashboardView(state, node.identity.value))
          .filter((item): item is HashboardHardwareData => !!item)
      : [];
  },
  getBayCount: () =>
    Math.max(
      0,
      ...[...(get().hardware.topology?.hashboards.values() ?? [])].map(
        (node: HashboardNode) => node.location?.bay ?? 0,
      ),
    ),
  getSlotByHbSn: (serial) => nodeForSerial(get().hardware, serial)?.location?.slot,
  getBayByHbSn: (serial) => nodeForSerial(get().hardware, serial)?.location?.bay,
  setAsics: (items) =>
    set((state) => {
      state.hardware.asics.clear();
      items.forEach((item) => upsertAsic(state.hardware, item));
    }),
  addAsic: (item) => set((state) => upsertAsic(state.hardware, item)),
  batchAddAsics: (items) => set((state) => items.forEach((item) => upsertAsic(state.hardware, item))),
  getAsic: (id) => {
    const asic = get().hardware.asics.get(asAsicId(id));
    return asic ? asicView(asic) : undefined;
  },
  getAsicsByHashboard: (serial) =>
    [...get().hardware.asics.values()].filter((asic) => asic.hashboardId === getHashboardId(serial)).map(asicView),
  getAsicPosition: (id) => {
    const asic = get().hardware.asics.get(asAsicId(id));
    return asic ? { row: asic.row, column: asic.column } : undefined;
  },
  getAsicRowsByHashboard: (serial) =>
    [
      ...new Set(
        [...get().hardware.asics.values()]
          .filter((asic) => asic.hashboardId === getHashboardId(serial))
          .map((asic) => asic.row)
          .filter((row): row is number => row !== undefined),
      ),
    ].sort((a, b) => a - b),
  linkAsicToHashboard: (asicId, serial) =>
    set((state) => {
      const node = nodeForSerial(state.hardware, serial);
      const id = asAsicId(asicId);
      if (node && !node.asicIds.includes(id)) node.asicIds.push(id);
    }),
  setPsus: (items) =>
    set((state) => {
      state.hardware.psus.clear();
      items.forEach((item) => upsertPsu(state.hardware, item));
    }),
  addPsu: (item) => set((state) => upsertPsu(state.hardware, item)),
  getPsu: (slot) => {
    const item = [...get().hardware.psus.values()].find((psu) => psu.slot === slot);
    return item
      ? {
          id: slot,
          slot,
          serial: item.serialNumber,
          manufacturer: item.manufacturer,
          model: item.model,
          hwRevision: item.hardwareRevision,
          firmware: item.firmware
            ? { appVersion: item.firmware.application?.version, bootloaderVersion: item.firmware.bootloader?.version }
            : undefined,
        }
      : undefined;
  },
  getAllPsus: () =>
    [...get().hardware.psus.values()].map((item) => ({
      id: item.slot ?? 0,
      slot: item.slot,
      serial: item.serialNumber,
      manufacturer: item.manufacturer,
      model: item.model,
      hwRevision: item.hardwareRevision,
      firmware: item.firmware
        ? { appVersion: item.firmware.application?.version, bootloaderVersion: item.firmware.bootloader?.version }
        : undefined,
    })),
  setFans: (items) =>
    set((state) => {
      state.hardware.fans.clear();
      items.forEach((item) => upsertFan(state.hardware, item));
    }),
  addFan: (item) => set((state) => upsertFan(state.hardware, item)),
  getFan: (slot) => {
    const item = [...get().hardware.fans.values()].find((fan) => fan.slot === slot);
    return item ? { slot, name: item.name } : undefined;
  },
  getAllFans: () => [...get().hardware.fans.values()].map((item) => ({ slot: item.slot ?? 0, name: item.name })),
});
