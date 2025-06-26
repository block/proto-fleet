import { useState } from "react";
import { Authentication as AuthenticationComponent } from ".";

interface AuthenticationArgs {
  isUpdateMode: boolean;
}

export const Authentication = ({ isUpdateMode }: AuthenticationArgs) => {
  const [isSubmitting, setIsSubmitting] = useState(false);

  return (
    <div>
      <AuthenticationComponent
        headline="Set up your admin login"
        description="Your admin login will be used to manage and make changes to this network’s miners, miner settings, and security configurations."
        submit={() => {}}
        isSubmitting={isSubmitting}
        setIsSubmitting={setIsSubmitting}
        isUpdateMode={isUpdateMode}
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Authentication",
  args: {
    isUpdateMode: false,
  },
};
