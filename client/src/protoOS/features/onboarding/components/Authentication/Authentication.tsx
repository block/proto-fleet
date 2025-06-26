import { useEffect, useState } from "react";
import { useLogin, usePassword, useSystemStatus } from "@/protoOS/api";
import { Authentication, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const navigate = useNavigate();
  const { setPassword } = usePassword();
  const login = useLogin();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const { data: systemStatus, pending: pendingSystemStatus } =
    useSystemStatus();

  useEffect(() => {
    if (!pendingSystemStatus && systemStatus?.onboarded !== undefined) {
      if (systemStatus.password_set) {
        navigate("/onboarding/mining-pool");
      }
    }
  }, [navigate, systemStatus, pendingSystemStatus]);

  function submit(password: string) {
    setPassword({
      password: password,
      onSuccess: () => {
        login({
          password: password,
          onFinally: () => {
            navigate("/onboarding/mining-pool");
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
        headline="Create an admin login for your miners"
        description="This password is required to modify performance settings or mining pool configurations for this miner."
      />
    </OnboardingLayout>
  );
};

export default AuthenticationPage;
