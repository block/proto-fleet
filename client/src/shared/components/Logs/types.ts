import { type logTypes } from "./constants";

export type logType = (typeof logTypes)[keyof typeof logTypes];

export type LogInfo = {
  message: string;
  logType: logType | null;
  timestamp: string | null;
};
