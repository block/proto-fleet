import { Alert } from "./types";
import { ColTitles } from "@/shared/components/List/types";

export const alertTypes = {
  controlBoard: "controlBoard",
  fan: "fan",
  hashboard: "hashboard",
  psu: "psu",
  pool: "pool",
};

export type AlertType = (typeof alertTypes)[keyof typeof alertTypes];

export const alertCols = {
  name: "minerName",
  status: "minerStatus",
  error: "message",
  timestamp: "timestamp",
};

export const alertColTitles = {
  [alertCols.name]: "Name",
  [alertCols.status]: "Status",
  [alertCols.error]: "Error",
  [alertCols.timestamp]: "",
} as ColTitles<keyof Alert>;

export const alertViews = {
  active: "active",
  archive: "archive",
};

export type AlertView = (typeof alertViews)[keyof typeof alertViews];
