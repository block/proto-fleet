import { Logo } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";

const OnboardingHeader = () => {
  return (
    <div className="fixed z-10 w-full">
      <div className="flex h-[60px] items-center border-b border-border-5 px-6">
        <Header icon={<Logo className="text-core-primary-fill" />} />
      </div>
    </div>
  );
};

export default OnboardingHeader;
