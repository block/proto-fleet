import Header from "components/Header";

import { Logo } from "icons";

const OnboardingHeader = () => {
  return (
    <div className="fixed w-full bg-surface-base z-10">
      <div className="border-b border-border-primary/5 px-6 h-[60px] flex items-center">
        <Header icon={<Logo />} />
      </div>
    </div>
  );
};

export default OnboardingHeader;
