import { ReactNode, useEffect, useState } from "react";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import { useLastPairingCompletedAt } from "@/protoFleet/store";
import { Alert, Dismiss } from "@/shared/assets/icons";
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
        <div className="flex size-8 items-center justify-center rounded-lg bg-core-primary-5">{icon}</div>
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

const AuthenticateMinersCard = ({
  count,
  onAuthenticationSuccess,
}: {
  count: number;
  onAuthenticationSuccess: () => void;
}) => {
  const [showAuthMinersModal, setShowAuthMinersModal] = useState(false);

  return (
    <>
      <TaskCard
        icon={<Alert className="text-text-critical" />}
        title="Authenticate miners"
        description={`${count} miner${count === 1 ? "" : "s"} need attention`}
        actionText="Authenticate"
        onActionClick={() => setShowAuthMinersModal(true)}
      />
      {showAuthMinersModal && (
        <AuthenticateMiners onClose={() => setShowAuthMinersModal(false)} onSuccess={onAuthenticationSuccess} />
      )}
    </>
  );
};

type CompleteSetupProps = {
  className?: string;
};

const CompleteSetup = ({ className = "" }: CompleteSetupProps) => {
  const [completSetupDismissed, setCompletSetupDismissed] = useReactiveLocalStorage<boolean>("completeSetupDismissed");

  const handleDismiss = () => {
    setCompletSetupDismissed(true);
  };

  // Fetch miners needing authentication to show in the "Authenticate miners" card
  const { totalMiners: authNeededCount, refetch: refetchAuthNeededMiners } = useAuthNeededMiners({
    pageSize: 100,
  });

  // Watch for pairing operations completing and refetch auth-needed miners
  const lastPairingCompletedAt = useLastPairingCompletedAt();
  useEffect(() => {
    if (lastPairingCompletedAt > 0) {
      refetchAuthNeededMiners();
    }
  }, [lastPairingCompletedAt, refetchAuthNeededMiners]);

  // Show complete setup banner only if there are miners needing authentication
  const shouldShow = !completSetupDismissed && authNeededCount > 0;

  return (
    <>
      {shouldShow && (
        <div className={className}>
          <div className="@container rounded-3xl bg-core-primary-5 p-6">
            <div className="mb-6 flex items-center justify-between gap-x-10">
              <div className="text-heading-300">Complete setup</div>
              <Button onClick={handleDismiss} variant="secondary" prefixIcon={<Dismiss />}></Button>
            </div>
            <div className="grid gap-4 @lg:grid-cols-2 @3xl:grid-cols-3 @7xl:grid-cols-4">
              <AuthenticateMinersCard count={authNeededCount} onAuthenticationSuccess={refetchAuthNeededMiners} />
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default CompleteSetup;
