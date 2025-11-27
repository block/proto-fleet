import { useCallback } from "react";
import { useNavigate as useReactNavigate } from "react-router-dom";
import Footer from "@/protoFleet/components/Footer";
import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";

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
        <div className="w-full max-w-100 p-6 phone:h-full">
          <LoginForm onSuccess={handleLoginSuccess} />
        </div>
      </div>
      <Footer />
    </div>
  );
};

export default Auth;
