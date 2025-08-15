import Footer from "@/protoFleet/components/Footer";
import LoginForm from "@/protoFleet/features/auth/components/LoginModal/LoginForm";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Auth = () => {
  const navigate = useNavigate();

  return (
    <div className="flex h-screen w-full flex-col bg-surface-base">
      <div className="flex flex-grow items-center-safe justify-center-safe">
        <div className="w-full max-w-100 p-6 phone:h-full">
          <LoginForm
            onSuccess={() => navigate("/")}
            onClickForgotPassword={() => null}
            onClickCreateAccount={() => navigate("/welcome")}
          />
        </div>
      </div>
      <Footer />
    </div>
  );
};

export default Auth;
