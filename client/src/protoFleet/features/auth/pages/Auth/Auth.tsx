import { useCallback } from "react";
import { useNavigate as useReactNavigate } from "react-router-dom";
import Footer from "@/protoFleet/components/Footer";
import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";
import Button from "@/shared/components/Button";

const Auth = () => {
  const navigate = useReactNavigate();

  const handleLoginSuccess = useCallback(
    (requiresPasswordChange: boolean) => {
      if (requiresPasswordChange) {
        navigate("/update-password");
      } else {
        navigate("/");
      }
    },
    [navigate],
  );

  return (
    <div className="flex h-screen w-full flex-col bg-surface-base">
      <div className="flex flex-grow items-center-safe justify-center-safe">
        <div className="flex w-full max-w-100 flex-col gap-6 p-6 phone:h-full">
          <LoginForm
            onSuccess={handleLoginSuccess}
            secondaryAction={<Button variant="textOnly" text="Join an account" to="/account-invitation" />}
          />
        </div>
      </div>
      <Footer />
    </div>
  );
};

export default Auth;
