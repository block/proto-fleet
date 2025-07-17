import { ReactNode, useState } from "react";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import { Alert, Dismiss, MiningPools, Racks } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";

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

const AuthenticateMinersCard = () => {
  const [showModal, setShowModal] = useState(false);

  return (
    <>
      <TaskCard
        icon={<Alert className="text-text-critical" />}
        title="Authenticate miners"
        description="17 miners need attention"
        actionText="Authenticate"
        onActionClick={() => setShowModal(true)}
      />
      {showModal && <AuthenticateMiners onClose={() => setShowModal(false)} />}
    </>
  );
};

const ConfigureMiningPoolsCard = () => {
  return (
    <TaskCard
      icon={<MiningPools />}
      title="Configure mining pools"
      description="123 miners"
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
  const [showCompleteSetup, setShowCompleteSetup] = useState(true);

  return (
    <>
      {showCompleteSetup && (
        <div className="@container rounded-3xl bg-landing-page p-6">
          <div className="mb-6 flex items-center justify-between gap-x-10">
            <div className="text-heading-300">Complete setup</div>
            <Button
              onClick={() => setShowCompleteSetup(false)}
              variant="secondary"
              prefixIcon={<Dismiss />}
            ></Button>
          </div>
          <div className="grid gap-4 @lg:grid-cols-2 @3xl:grid-cols-3 @7xl:grid-cols-4">
            <AuthenticateMinersCard />
            <ConfigureMiningPoolsCard />
            <SetUpRacksCard />
          </div>
        </div>
      )}
    </>
  );
};

export default CompleteSetup;
