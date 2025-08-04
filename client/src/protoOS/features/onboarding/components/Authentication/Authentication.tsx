import { useEffect, useState } from "react";
import { useLogin, usePassword, useSystemStatus } from "@/protoOS/api";
import { Authentication, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const navigate = useNavigate();
  const { setPassword } = usePassword();
  const login = useLogin();
  const [submitError, setSubmitError] = useState<string | undefined>();
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
    setIsSubmitting(true);
    setSubmitError(undefined);
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
      onError: (message) => {
        setSubmitError(message);
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }

  return (
    <OnboardingLayout>
      <Authentication
        submit={submit}
        submitError={submitError}
        isSubmitting={isSubmitting}
        setIsSubmitting={setIsSubmitting}
        headline="Create an admin login for your miner"
        description="This password is required to modify performance settings or mining pool configurations for this miner."
        initUsername="admin"
      />
    </OnboardingLayout>
  );
};

export default AuthenticationPage;
