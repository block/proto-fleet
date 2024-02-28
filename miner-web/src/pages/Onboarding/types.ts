import { fanModes, info, tabs } from "./constants";

export type PoolInfo = Record<keyof typeof info, any>;

export type Tabs = keyof typeof tabs;

export type DefaultPoolIndex = 0;

export type BackupPoolIndex = 1 | 2;

export type PoolIndex = DefaultPoolIndex | BackupPoolIndex;

export type FanMode = keyof typeof fanModes;
