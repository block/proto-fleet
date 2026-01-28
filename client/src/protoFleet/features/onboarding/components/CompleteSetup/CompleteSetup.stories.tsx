import { ReactNode } from "react";
import { action } from "storybook/actions";
import { Alert, MiningPools } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";

type TaskCardProps = {
  icon: ReactNode;
  title: string;
  description?: string;
  actionText?: string;
  onActionClick?: () => void;
  skippable?: boolean;
  onSkip?: () => void;
  isLoading?: boolean;
};

const TaskCard = ({
  icon,
  title,
  description,
  actionText,
  onActionClick,
  skippable = false,
  onSkip,
  isLoading = false,
}: TaskCardProps) => {
  return (
    <div className="flex flex-col justify-between gap-4 rounded-2xl bg-surface-base p-6">
      <div className="flex flex-col gap-4">
        <div className="flex size-8 items-center justify-center rounded-lg bg-surface-5">{icon}</div>
        <div className="flex flex-col">
          <div className="text-emphasis-300">{title}</div>
          {description && <div className="text-300">{description}</div>}
        </div>
      </div>
      <div className="flex justify-between gap-5">
        {skippable && (
          <Button className="pl-0" variant="textOnly" onClick={onSkip} disabled={isLoading}>
            Skip
          </Button>
        )}
        <Button
          onClick={onActionClick}
          variant={skippable ? "secondary" : "primary"}
          className={skippable ? "" : "w-full"}
          disabled={isLoading}
          loading={isLoading}
        >
          {actionText}
        </Button>
      </div>
    </div>
  );
};

type CompleteSetupStoryProps = {
  poolNeededCount: number;
  authNeededCount: number;
  isLoading?: boolean;
};

const CompleteSetupStory = ({ poolNeededCount, authNeededCount, isLoading = false }: CompleteSetupStoryProps) => {
  const hasConfigurePoolCard = poolNeededCount > 0;
  const hasAuthCard = authNeededCount > 0;

  if (!hasConfigurePoolCard && !hasAuthCard) {
    return null;
  }

  return (
    <div className="p-8">
      <div className="@container rounded-3xl bg-core-primary-5 p-6">
        <div className="mb-6 flex items-center justify-between gap-x-10">
          <div className="text-heading-300">Complete setup</div>
          <Button onClick={action("dismiss complete setup")} variant="secondary" prefixIcon={<div>×</div>}></Button>
        </div>
        <div className="grid gap-4 @lg:grid-cols-2 @3xl:grid-cols-3 @7xl:grid-cols-4">
          {hasConfigurePoolCard && (
            <TaskCard
              icon={<MiningPools className="text-text-primary" />}
              title="Configure pools"
              description={`${poolNeededCount} ${poolNeededCount === 1 ? "miner" : "miners"}`}
              actionText="Configure"
              onActionClick={action("configure pools")}
              skippable
              onSkip={action("skip configure pools")}
              isLoading={isLoading}
            />
          )}
          {hasAuthCard && (
            <TaskCard
              icon={<Alert className="text-text-critical" />}
              title="Authenticate miners"
              description={`${authNeededCount} miner${authNeededCount === 1 ? "" : "s"} ${authNeededCount === 1 ? "needs" : "need"} attention`}
              actionText="Authenticate"
              onActionClick={action("authenticate miners")}
            />
          )}
        </div>
      </div>
    </div>
  );
};

export const BothCards = () => <CompleteSetupStory poolNeededCount={6} authNeededCount={3} />;

export const OnlyConfigurePools = () => <CompleteSetupStory poolNeededCount={14} authNeededCount={0} />;

export const OnlyAuthenticateMiners = () => <CompleteSetupStory poolNeededCount={0} authNeededCount={5} />;

export const ConfigurePoolsLoading = () => (
  <CompleteSetupStory poolNeededCount={8} authNeededCount={0} isLoading={true} />
);

export const SingleMiner = () => <CompleteSetupStory poolNeededCount={1} authNeededCount={0} />;

export const ManyMiners = () => <CompleteSetupStory poolNeededCount={127} authNeededCount={0} />;

export default {
  title: "Proto Fleet/Onboarding/Complete Setup",
};
