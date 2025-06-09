import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { useAuthContext } from "@/protoFleet/features/auth/contexts/AuthContext";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

const PAIR_MINERS_PROMPT = {
  title: "Finish setting up your fleet",
  subtitle:
    "You’ve started configuring a new fleet on this network, but haven’t added any miners yet. Once miners are added, you’ll see live data and fleet performance here.",
  cta: "Add Miners",
  route: "/onboarding/miners",
};

const CONFIGURE_POOL_PROMPT = {
  title: "Finish setting up your fleet",
  subtitle:
    "You’ve added miners to your fleet, but haven’t configured a pool yet. Once you configure a pool, you’ll see live data and fleet performance here.",
  cta: "Configure Pool",
  route: "/onboarding/settings",
};

type CompleteOnboardingDialogProps = {
  onboardingStatus: FleetOnboardingStatus | null;
};

const CompleteOnboardingDialog = ({
  onboardingStatus,
}: CompleteOnboardingDialogProps) => {
  const { setAuthTokens } = useAuthContext();
  const navigate = useNavigate();

  const completeOnboarding: {
    title: string;
    subtitle: string;
    cta: string;
    route: string;
  } | null = useMemo(() => {
    if (
      onboardingStatus === null ||
      (onboardingStatus.devicePaired === true &&
        onboardingStatus.poolConfigured === true)
    ) {
      return null;
    }

    if (onboardingStatus.devicePaired == false) {
      return PAIR_MINERS_PROMPT;
    } else if (onboardingStatus.poolConfigured == false) {
      return CONFIGURE_POOL_PROMPT;
    }

    return null;
  }, [onboardingStatus]);

  return (
    <Dialog
      show={true}
      title={completeOnboarding?.title || ""}
      titleSize="text-heading-200"
      subtitle={completeOnboarding?.subtitle || ""}
      subtitleSize="text-300"
      buttons={[
        {
          text: "Logout",
          onClick: () => {
            setAuthTokens({
              accessToken: { value: "", expiry: new Date() },
            });
          },
          variant: variants.secondary,
        },
        {
          text: completeOnboarding?.cta || "Continue",
          onClick: () => {
            if (completeOnboarding?.route) {
              navigate(completeOnboarding.route);
            }
          },
          variant: variants.accent,
        },
      ]}
    />
  );
};
export default CompleteOnboardingDialog;
