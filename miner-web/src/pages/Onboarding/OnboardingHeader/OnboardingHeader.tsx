import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import { sizes } from "components/Button";
import { ButtonProps } from "components/ButtonGroup";
import Header from "components/Header";

import { Logo, Pause } from "icons";

interface OnboardingHeaderProps {
  button?: ButtonProps;
  openMenu: () => void;
}

const OnboardingHeader = ({ button, openMenu }: OnboardingHeaderProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  return (
    <div className="fixed w-full bg-surface-base z-10">
      <div className="border-b border-border-primary/5 px-6 h-[60px] flex items-center">
        <Header
          icon={
            <>
              {(isPhone || isTablet) && (
                <Pause
                  className="mr-2 text-text-primary hover:cursor-pointer"
                  onClick={openMenu}
                />
              )}
              <Logo />
            </>
          }
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
