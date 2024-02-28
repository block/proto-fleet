import { ReactNode } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "components/Button";
import ButtonGroup, {
  ButtonProps,
  groupVariants,
} from "components/ButtonGroup";

interface HeaderProps {
  buttons?: ButtonProps[];
  buttonSize?: keyof typeof sizes;
  centerButton?: boolean;
  className?: string;
  icon?: ReactNode;
  iconOnClick?: () => void;
  inline?: boolean;
  subtitleSize?: string;
  subtitle?: string;
  title?: string;
  titleSize?: string;
}

const Header = ({
  buttons,
  buttonSize = sizes.base,
  centerButton,
  className,
  icon,
  iconOnClick,
  inline = false,
  subtitle,
  subtitleSize = "text-heading-100",
  title,
  titleSize = "text-heading-100",
}: HeaderProps) => {
  return (
    <div
      className={clsx(
        "flex justify-between bg-surface-base w-full",
        { "items-center": centerButton },
        className
      )}
    >
      <div className={clsx({ "flex items-center": inline })}>
        {icon && iconOnClick && (
          <Button
            variant={variants.secondary}
            size={sizes.base}
            prefixIcon={icon}
            onClick={iconOnClick}
          />
        )}
        {icon && !iconOnClick && icon}
        {title && (
          <div
            className={clsx("text-text-primary", titleSize, {
              "ml-4": (icon || iconOnClick) && inline,
              "mt-3": (icon || iconOnClick) && !inline,
              "mb-1": subtitle,
            })}
          >
            {title}
          </div>
        )}
        {subtitle && (
          <div className={clsx("text-text-primary/70", subtitleSize)}>
            {subtitle}
          </div>
        )}
      </div>
      {buttons && (
        <ButtonGroup
          buttons={buttons}
          variant={groupVariants.rightAligned}
          size={buttonSize}
        />
      )}
    </div>
  );
};

export default Header;
