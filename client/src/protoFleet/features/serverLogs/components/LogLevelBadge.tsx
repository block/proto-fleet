import clsx from "clsx";

import { LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";

// Tailwind class bundles per level. Kept as static strings (not built up
// by template literals) so the JIT compiler can statically detect them
// and the corresponding utility classes survive purging.
const styles: Record<LogLevel, string> = {
  [LogLevel.UNSPECIFIED]: "bg-core-primary-10 text-text-primary-50",
  [LogLevel.DEBUG]: "bg-core-primary-10 text-text-primary-50",
  [LogLevel.INFO]: "bg-intent-info-10 text-intent-info-text",
  [LogLevel.WARN]: "bg-intent-warning-10 text-intent-warning-text",
  [LogLevel.ERROR]: "bg-intent-critical-20 text-intent-critical-text",
};

const labels: Record<LogLevel, string> = {
  [LogLevel.UNSPECIFIED]: "—",
  [LogLevel.DEBUG]: "DEBUG",
  [LogLevel.INFO]: "INFO",
  [LogLevel.WARN]: "WARN",
  [LogLevel.ERROR]: "ERROR",
};

interface LogLevelBadgeProps {
  level: LogLevel;
  className?: string;
}

const LogLevelBadge = ({ level, className }: LogLevelBadgeProps) => {
  return (
    <span
      className={clsx(
        "inline-flex w-[64px] shrink-0 items-center justify-center rounded px-1.5 py-0.5 font-mono text-[11px] font-semibold tracking-wide",
        styles[level],
        className,
      )}
    >
      {labels[level]}
    </span>
  );
};

export default LogLevelBadge;
