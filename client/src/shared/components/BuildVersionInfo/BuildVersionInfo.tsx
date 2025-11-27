import { buildVersionInfo } from "@/shared/utils/version";

export interface BuildVersionInfoProps {
  /** Show only version number without additional details */
  compact?: boolean;
}

/**
 * Component to display application build version information
 */
const BuildVersionInfo = ({ compact = false }: BuildVersionInfoProps) => {
  const { version, buildDate, commit } = buildVersionInfo;

  const formatBuildDate = (dateString: string): string => {
    if (dateString === "unknown") return "Unknown";

    try {
      return new Date(dateString).toLocaleString();
    } catch {
      return dateString;
    }
  };

  if (compact) {
    return (
      <div className="mx-auto text-xs text-text-primary-30" aria-label={`Version ${version}`}>
        {version}
      </div>
    );
  }

  return (
    <div className="mx-auto text-xs text-text-primary-30">
      <div>Version: {version}</div>
      <div>Built: {formatBuildDate(buildDate)}</div>
      <div>Commit: {commit}</div>
    </div>
  );
};

export default BuildVersionInfo;
