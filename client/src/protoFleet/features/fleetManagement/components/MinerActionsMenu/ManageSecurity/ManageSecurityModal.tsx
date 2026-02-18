import { useMemo } from "react";
import { DismissCircleDark, Fleet, LogoAlt, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Header from "@/shared/components/Header";
import PageOverlay from "@/shared/components/PageOverlay";
import Row from "@/shared/components/Row";

export interface MinerGroup {
  name: string;
  model: string;
  manufacturer: string;
  count: number;
  deviceIdentifiers: string[];
  status: "pending" | "updated" | "failed";
  successCount?: number;
  failureCount?: number;
}

interface ManageSecurityModalProps {
  show: boolean;
  minerGroups: MinerGroup[];
  onUpdateGroup: (group: MinerGroup) => void;
  onRevertGroup?: (group: MinerGroup) => void;
  onDismiss: () => void;
  onDone: () => void;
}

const ManageSecurityModal = ({
  show,
  minerGroups,
  onUpdateGroup,
  onRevertGroup,
  onDismiss,
  onDone,
}: ManageSecurityModalProps) => {
  // Sort groups: Proto rigs first, then alphabetically by model
  const sortedGroups = useMemo(() => {
    return [...minerGroups].sort((a, b) => {
      // Proto rigs always come first
      if (a.manufacturer === "proto" && b.manufacturer !== "proto") return -1;
      if (a.manufacturer !== "proto" && b.manufacturer === "proto") return 1;
      // Otherwise sort alphabetically by model
      return a.model.localeCompare(b.model);
    });
  }, [minerGroups]);

  // Determine if any updates have been made
  const hasUpdates = minerGroups.some((group) => group.status !== "pending");

  const getIconForGroup = (group: MinerGroup) => {
    if (group.manufacturer === "proto") {
      return <LogoAlt width={iconSizes.medium} />;
    }
    return <Fleet width={iconSizes.medium} />;
  };

  const getStatusIcon = (group: MinerGroup) => {
    if (group.status === "updated") {
      return (
        <div className="text-intent-positive">
          <Success width={iconSizes.small} />
        </div>
      );
    }
    if (group.status === "failed") {
      return (
        <div className="text-intent-critical">
          <Success width={iconSizes.small} />
        </div>
      );
    }
    return null;
  };

  const getActionButton = (group: MinerGroup) => {
    if (group.status === "pending") {
      return (
        <Button variant={variants.secondary} onClick={() => onUpdateGroup(group)}>
          Update
        </Button>
      );
    }
    if (group.status === "updated" && onRevertGroup) {
      return (
        <Button variant={variants.secondary} onClick={() => onRevertGroup(group)} disabled>
          Revert changes
        </Button>
      );
    }
    return null;
  };

  return (
    <PageOverlay show={show}>
      <div className="h-full w-full overflow-auto bg-surface-base px-6 pt-4 pb-6">
        <Header
          className="sticky top-0 z-10 pb-14"
          title="Manage security"
          titleSize="text-heading-100"
          icon={<DismissCircleDark width="w-[24px]" onClick={onDismiss} className="cursor-pointer" />}
          inline
          buttonSize={sizes.base}
          buttons={[
            {
              text: "Done",
              variant: variants.primary,
              onClick: onDone,
            },
          ]}
        />

        <div className="mx-auto max-w-[800px]">
          <div className="mb-6">
            <h1 className="text-heading-300 text-text-primary">Update the admin login for your miners</h1>
            <p className="text-300 text-text-primary-70">
              This password will be required to make any changes to pools or miner performance.
            </p>
          </div>

          <div className="flex flex-col">
            {sortedGroups.map((group, index) => (
              <div key={`${group.manufacturer}-${group.model}`}>
                <Row
                  prefixIcon={getIconForGroup(group)}
                  suffixIcon={
                    <div className="flex items-center gap-4">
                      <span className="text-text-secondary text-300 whitespace-nowrap">
                        {group.count} {group.count === 1 ? "miner" : "miners"}
                      </span>
                      {getStatusIcon(group)}
                      {getActionButton(group)}
                    </div>
                  }
                  divider={false}
                >
                  <div className="text-emphasis-300 text-text-primary">{group.name}</div>
                </Row>
                {index < sortedGroups.length - 1 && <Divider />}
              </div>
            ))}
          </div>

          {hasUpdates && (
            <div className="bg-intent-positive-10 text-intent-positive-text mt-6 rounded-lg px-3 py-2 text-200">
              Updates have been applied to selected groups
            </div>
          )}
        </div>
      </div>
    </PageOverlay>
  );
};

export default ManageSecurityModal;
