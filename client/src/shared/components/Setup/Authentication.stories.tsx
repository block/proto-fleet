import { useState } from "react";
import { action } from "storybook/actions";
import { Authentication as AuthenticationComponent } from ".";

interface AuthenticationArgs {
  inputPrefix?: string;
  initUsername?: string;
  isUpdateMode: boolean;
  requirePasswordConfirmation: boolean;
}

export const Authentication = ({
  inputPrefix,
  initUsername,
  isUpdateMode,
  requirePasswordConfirmation,
}: AuthenticationArgs) => {
  const [isSubmitting, setIsSubmitting] = useState(false);

  return (
    <div>
      <AuthenticationComponent
        headline="Set up your admin login"
        description="Your admin login will be used to manage and make changes to this network’s miners, miner settings, and security configurations."
        inputPrefix={inputPrefix}
        initUsername={initUsername}
        submit={action("submit")}
        isSubmitting={isSubmitting}
        setIsSubmitting={setIsSubmitting}
        isUpdateMode={isUpdateMode}
        requirePasswordConfirmation={requirePasswordConfirmation}
      />
    </div>
  );
};

export default {
  title: "Shared/Setup/Authentication",
  args: {
    inputPrefix: "Fleet",
    initUsername: "admin",
    isUpdateMode: false,
    requirePasswordConfirmation: true,
  },
};
