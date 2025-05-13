import { useLogin } from "@/protoFleet/api/useLogin";
import { usePassword } from "@/protoFleet/api/usePassword";
import { Authentication, SetupHeader } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const setPassword = usePassword();
  const login = useLogin();
  const navigate = useNavigate();

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
    <div>
      <SetupHeader />
      <Authentication
        submit={submit}
        headline="Set up your admin login"
        description="Your admin login will be used to manage and make changes to this network’s miners, miner settings, and security configurations."
      />
    </div>
  );
};

export default AuthenticationPage;
