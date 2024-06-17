export type LogInfo = {
  isDebug?: boolean;
  isError?: boolean;
  isInfo?: boolean;
  isWarning?: boolean;
  message: string;
  timestamp: string | null;
};
