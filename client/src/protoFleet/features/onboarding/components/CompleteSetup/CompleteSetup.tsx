import { ReactNode, useState } from "react";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import { useMinerIds } from "@/protoFleet/store";
import { Alert, Dismiss, MiningPools, Racks } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

type TaskCardProps = {
  icon: ReactNode;
  title: string;
  description?: string;
  actionText?: string;
  onActionClick?: () => void;
  skippable?: boolean;
  onSkip?: () => void;
};

const TaskCard = ({
  icon,
  title,
  description,
  actionText,
  onActionClick,
  skippable = false,
  onSkip,
}: TaskCardProps) => {
  return (
    <div className="flex flex-col justify-between gap-4 rounded-2xl bg-surface-base p-6">
      <div className="flex flex-col gap-4">
        <div className="flex size-8 items-center justify-center rounded-lg bg-surface-5">
          {icon}
        </div>
        <div className="flex flex-col">
          <div className="text-emphasis-300">{title}</div>
          {description && <div className="text-300">{description}</div>}
        </div>
      </div>
      <div className="flex justify-between gap-5">
        {skippable && (
          <Button className="pl-0" variant="textOnly" onClick={onSkip}>
            Skip
          </Button>
        )}
        <Button
          onClick={onActionClick}
          variant={skippable ? "secondary" : "primary"}
          className={skippable ? "" : "w-full"}
        >
          {actionText}
        </Button>
      </div>
    </div>
  );
};

const AuthenticateMinersCard = ({ minerIds }: { minerIds: string[] }) => {
  const [showAuthMinersModal, setShowAuthMinersModal] = useState(false);

  return (
    <>
      <TaskCard
        icon={<Alert className="text-text-critical" />}
        title="Authenticate miners"
        description={`${minerIds.length} miner${
          minerIds.length === 1 ? "" : "s"
        } need attention`}
        actionText="Authenticate"
        onActionClick={() => setShowAuthMinersModal(true)}
      />
      {showAuthMinersModal && (
        <AuthenticateMiners onClose={() => setShowAuthMinersModal(false)} />
      )}
    </>
  );
};

const ConfigureMiningPoolsCard = ({ minerIds }: { minerIds: string[] }) => {
  return (
    <TaskCard
      icon={<MiningPools />}
      title="Configure mining pools"
      description={`${minerIds.length} miner${
        minerIds.length === 1 ? "" : "s"
      }`}
      actionText="Configure"
      skippable={true}
    />
  );
};

const SetUpRacksCard = () => {
  return (
    <TaskCard
      icon={<Racks />}
      title="Set up racks"
      actionText="Set up"
      skippable={true}
    />
  );
};

const CompleteSetup = () => {
  const [completSetupDismissed, setCompletSetupDismissed] =
    useReactiveLocalStorage<boolean>("completeSetupDismissed");

  const handleDismiss = () => {
    setCompletSetupDismissed(true);
  };

  // TODO: remove this placeholder once we have a way to get the number of unauthenticated miners
  const minerIds = useMinerIds();

  return (
    <>
      {!completSetupDismissed && (
        <div className="@container rounded-3xl bg-landing-page p-6">
          <div className="mb-6 flex items-center justify-between gap-x-10">
            <div className="text-heading-300">Complete setup</div>
            <Button
              onClick={handleDismiss}
              variant="secondary"
              prefixIcon={<Dismiss />}
            ></Button>
          </div>
          <div className="grid gap-4 @lg:grid-cols-2 @3xl:grid-cols-3 @7xl:grid-cols-4">
            <AuthenticateMinersCard minerIds={minerIds} />
            <ConfigureMiningPoolsCard minerIds={minerIds} />
            <SetUpRacksCard />
          </div>
        </div>
      )}
    </>
  );
};

export default CompleteSetup;
