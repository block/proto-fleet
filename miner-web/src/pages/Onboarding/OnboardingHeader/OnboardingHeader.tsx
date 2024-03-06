import { sizes } from "components/Button";
import { ButtonProps } from "components/ButtonGroup";
import Header from "components/Header";

import { Logo } from "icons";

interface OnboardingHeaderProps {
  button?: ButtonProps;
}

const OnboardingHeader = ({ button }: OnboardingHeaderProps) => {
  return (
    <div className="fixed w-full">
      <div className="border-b border-border-primary/5 px-6 py-3 flex items-center">
        <Header
          icon={<Logo />}
          buttons={button ? [button] : undefined}
          buttonSize={sizes.compact}
          centerButton
          inline
        />
      </div>
    </div>
  );
};

export default OnboardingHeader;
