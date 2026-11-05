import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";

import { MinerListFilterSchema } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import { useAccountMutation } from "@/protoFleet/features/hostedAccounts/useAccountMutation";
import { STEP_KEYS, STEPS } from "@/protoFleet/features/onboarding/constants";
import ConfigurationLibrary from "@/protoFleet/features/poolConfigurations/ConfigurationLibrary";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import Button from "@/shared/components/Button";
import { OnboardingLayout } from "@/shared/components/Setup";

const SettingsPage = () => {
  const navigate = useNavigate();
  const { refetch } = useOnboardedStatus();
  const [applying, setApplying] = useState(false);
  const completion = useAccountMutation();
  return (
    <OnboardingLayout steps={STEPS} currentStep={STEP_KEYS.settings}>
      <div className="flex flex-col gap-6">
        <ConfigurationLibrary />
        {completion.error ? <p role="alert">{completion.error}</p> : null}
        <div className="flex flex-wrap justify-end gap-4">
          <Button variant="secondary" text="Configure existing miners" onClick={() => setApplying(true)} />
          <Button
            variant="primary"
            text="Complete setup"
            loading={completion.busy}
            onClick={() => {
              void completion.run(async () => {
                const status = await refetch();
                if (!status?.poolConfigured) throw new Error("Create a pool configuration to complete setup.");
                navigate("/");
              });
            }}
          />
        </div>
      </div>
      {applying ? (
        <ConfigurationSelector
          deviceIdentifiers={[]}
          filter={create(MinerListFilterSchema)}
          onDismiss={() => setApplying(false)}
        />
      ) : null}
    </OnboardingLayout>
  );
};

export default SettingsPage;
