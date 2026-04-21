import { useState } from "react";
import { useNavigate } from "react-router-dom";

import MiningPoolsForm from "@/protoFleet/components/MiningPools";
import { STEP_KEYS, STEPS } from "@/protoFleet/features/onboarding/constants";

import Header from "@/shared/components/Header";
import { BootingUp, OnboardingLayout } from "@/shared/components/Setup";

const SettingsPage = () => {
  const navigate = useNavigate();
  const [settingUpMiner, setSettingUpMiner] = useState(false);

  if (settingUpMiner) {
    return <BootingUp title="Configuring your fleet" />;
  }

  return (
    <OnboardingLayout steps={STEPS} currentStep={STEP_KEYS.settings}>
      <Header
        className="mb-6"
        title="Miner settings"
        titleSize="text-heading-300"
        description={
          <>
            {"These will be your "}
            <span className="text-emphasis-300">default settings for new miners added to this network.</span>
            <br className="phone:hidden" />
            You can always edit these or create custom settings for new miners.
          </>
        }
        inline
      />
      <MiningPoolsForm
        buttonLabel="Complete setup"
        onSaveRequested={() => setSettingUpMiner(true)}
        onSaveDone={() => navigate("/")}
      />
    </OnboardingLayout>
  );
};

export default SettingsPage;
