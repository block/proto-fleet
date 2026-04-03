import Footer from "@/protoFleet/components/Footer";
import { Logo } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface UpdatePasswordSuccessProps {
  onLogin: () => void;
}

export const UpdatePasswordSuccess = ({ onLogin }: UpdatePasswordSuccessProps) => {
  return (
    <div className="flex h-screen w-full flex-col bg-surface-base">
      <div className="flex flex-grow items-center-safe justify-center-safe">
        <div className="w-full max-w-100 p-6 phone:h-full">
          <div className="flex flex-col gap-10">
            <Logo width="w-[86px]" />
            <div className="flex flex-col gap-6">
              <Header title="Password saved" titleSize="text-heading-300" description="Password updated." />
              <Button onClick={onLogin} variant="primary">
                Login
              </Button>
            </div>
          </div>
        </div>
      </div>
      <Footer />
    </div>
  );
};
