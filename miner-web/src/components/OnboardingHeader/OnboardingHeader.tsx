import Header from "components/Header";

import { Logo } from "icons";

const OnboardingHeader = () => {
  return (
    <div className="fixed w-full z-10">
      <div className="border-b border-border-5 px-6 h-[60px] flex items-center">
        <Header icon={<Logo className="text-core-primary-fill" />} />
      </div>
    </div>
  );
};

export default OnboardingHeader;
