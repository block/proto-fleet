import { STEP_KEYS, STEPS } from "../../constants";
import Button, { variants } from "@/shared/components/Button";
import { OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const SecurityPage = () => {
  const navigate = useNavigate();

  return (
    <OnboardingLayout steps={STEPS} currentStep={STEP_KEYS.security}>
      <h1 className="text-heading-300">Security TBD</h1>
      <p>Waiting on final product requirements and designs for authenticating miners with protoFleet</p>
      <div className="mt-6 flex justify-end">
        <Button onClick={() => navigate("/onboarding/settings")} variant={variants.primary}>
          Continue
        </Button>
      </div>
    </OnboardingLayout>
  );
};

export default SecurityPage;
