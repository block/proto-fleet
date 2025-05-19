import { useState } from "react";
import { useLogin } from "@/protoFleet/api/useLogin";
import { usePassword } from "@/protoFleet/api/usePassword";
import { Authentication, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const { setPassword } = usePassword();
  const login = useLogin();
  const navigate = useNavigate();
  const [isSubmitting, setIsSubmitting] = useState(false);

  function submit(password: string) {
    setPassword({
      password: password,
      onSuccess: () => {
        login({
          password: password,
          onFinally: () => {
            navigate("/onboarding/miners");
          },
        });
      },
    });
  }

  return (
    <OnboardingLayout>
      <Authentication
        submit={submit}
        isSubmitting={isSubmitting}
        setIsSubmitting={setIsSubmitting}
        headline="Set up your admin login"
        description="Your admin login will be used to manage and make changes to this network’s miners, miner settings, and security configurations."
      />
    </OnboardingLayout>
  );
};

export default AuthenticationPage;
