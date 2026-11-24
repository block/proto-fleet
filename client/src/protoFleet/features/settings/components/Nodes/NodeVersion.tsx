import { getNodeUpgradeStatus } from "./getNodeDisplayStatus";
import type { NodeUpgradeStatus } from "./getNodeDisplayStatus";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

const UPGRADE_CONFIG: Record<NodeUpgradeStatus, { label: string; iconClass: string }> = {
  required: { label: "Upgrade required", iconClass: "text-intent-critical-fill" },
  recommended: { label: "Upgrade recommended", iconClass: "text-intent-warning-fill" },
};

interface NodeVersionProps {
  node: FleetNodeItem;
  // Spell the upgrade state out where there is room; otherwise it is the
  // icon's hover and screen-reader text.
  showUpgradeLabel?: boolean;
}

const NodeVersion = ({ node, showUpgradeLabel = false }: NodeVersionProps) => {
  const upgradeStatus = getNodeUpgradeStatus(node);
  const upgrade = upgradeStatus ? UPGRADE_CONFIG[upgradeStatus] : null;
  return (
    <span title={upgrade && !showUpgradeLabel ? upgrade.label : undefined}>
      <span className="break-all">{node.version || "—"}</span>
      {upgrade ? (
        <span className="ml-1.5 inline-flex items-center gap-1 align-middle whitespace-nowrap text-text-primary-50">
          <Alert width={iconSizes.small} className={upgrade.iconClass} />
          <span className={showUpgradeLabel ? undefined : "sr-only"}>{upgrade.label}</span>
        </span>
      ) : null}
    </span>
  );
};

export default NodeVersion;
