import { useMemo } from "react";
import { minerTypes } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { DismissCircleDark, Fleet, LogoAlt, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { variants } from "@/shared/components/Button";
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
  status: "pending" | "loading" | "updated" | "failed";
}

const getGroupStatusFlags = (status: MinerGroup["status"]) => ({
  isPending: status === "pending",
  isLoading: status === "loading",
  isFailed: status === "failed",
});

interface ManageSecurityModalProps {
  open: boolean;
  minerGroups: MinerGroup[];
  onUpdateGroup: (group: MinerGroup) => void;
  onDismiss: () => void;
  onDone: () => void;
}

const ManageSecurityModal = ({ open, minerGroups, onUpdateGroup, onDismiss, onDone }: ManageSecurityModalProps) => {
  const sortedGroups = useMemo(() => {
    return [...minerGroups].sort((a, b) => {
      // Proto rigs always come first
      if (a.manufacturer.toLowerCase() === minerTypes.protoRig && b.manufacturer.toLowerCase() !== minerTypes.protoRig)
        return -1;
      if (a.manufacturer.toLowerCase() !== minerTypes.protoRig && b.manufacturer.toLowerCase() === minerTypes.protoRig)
        return 1;
      // Otherwise sort alphabetically by model
      return a.model.localeCompare(b.model);
    });
  }, [minerGroups]);

  const getIconForGroup = (group: MinerGroup) => {
    if (group.status === "updated") {
      return (
        <div className="text-intent-success-fill">
          <Success width={iconSizes.medium} />
        </div>
      );
    }
    if (group.manufacturer.toLowerCase() === minerTypes.protoRig) {
      return <LogoAlt width={iconSizes.medium} />;
    }
    return <Fleet width={iconSizes.medium} />;
  };

  const getActionButton = (group: MinerGroup) => {
    const { isPending, isLoading, isFailed } = getGroupStatusFlags(group.status);

    if (isPending || isLoading || isFailed) {
      return (
        <Button variant={variants.secondary} onClick={() => onUpdateGroup(group)} loading={isLoading}>
          Update
        </Button>
      );
    }
    return null;
  };

  return (
    <PageOverlay open={open}>
      <div className="h-full w-full overflow-auto bg-surface-base px-6 pt-4 pb-6">
        <Header
          className="sticky top-0 z-10 pb-14"
          title="Manage security"
          titleSize="text-heading-300"
          icon={<DismissCircleDark ariaLabel="Close manage security" width="w-6" onClick={onDismiss} />}
          inline
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
        </div>
      </div>
    </PageOverlay>
  );
};

export default ManageSecurityModal;
