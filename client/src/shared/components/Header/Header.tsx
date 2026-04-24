import { ReactNode } from "react";
import clsx from "clsx";

import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";
import ButtonGroup, { ButtonProps, groupVariants } from "@/shared/components/ButtonGroup";

interface BaseHeaderProps {
  buttons?: ButtonProps[];
  buttonSize?: keyof typeof sizes;
  buttonsWrapperClassName?: string;
  centerButton?: boolean;
  children?: ReactNode;
  className?: string;
  compact?: boolean;
  descriptionClassName?: string;
  iconButtonClassName?: string;
  iconSize?: keyof typeof sizes;
  iconTextColor?: string;
  iconVariant?: ButtonVariant;
  inline?: boolean;
  stackButtonsOnPhone?: boolean;
  showSubtitleTooltip?: boolean;
  subtitle?: string;
  subtitleClassName?: string;
  subtitleSize?: string;
  testId?: string;
  title?: string | ReactNode;
  titleSize?: string;
  eybrow?: string;
  description?: string | ReactNode;
}

type StaticHeaderIconProps = {
  icon?: ReactNode;
  iconAriaLabel?: undefined;
  iconOnClick?: undefined;
};

type InteractiveHeaderIconProps = {
  icon: ReactNode;
  iconAriaLabel: string;
  iconOnClick: () => void;
};

type HeaderProps = BaseHeaderProps & (StaticHeaderIconProps | InteractiveHeaderIconProps);

const Header = ({
  buttons,
  buttonSize = sizes.base,
  buttonsWrapperClassName,
  centerButton,
  className,
  children,
  compact,
  descriptionClassName,
  iconButtonClassName,
  iconAriaLabel,
  icon,
  iconOnClick,
  iconSize = sizes.base,
  iconTextColor,
  iconVariant = variants.secondary,
  inline = false,
  stackButtonsOnPhone = true,
  showSubtitleTooltip,
  subtitle,
  subtitleClassName,
  subtitleSize = "text-heading-100",
  testId,
  title,
  titleSize = "text-heading-100",
  eybrow,
  description,
}: HeaderProps) => {
  return (
    <div
      className={clsx(
        "flex w-full justify-between gap-3",
        { "items-center": centerButton, "phone:flex-wrap": stackButtonsOnPhone },
        className,
      )}
    >
      <div className={clsx("w-full", { "flex items-center": inline })}>
        {icon && iconOnClick ? (
          <Button
            ariaLabel={iconAriaLabel}
            textColor={iconTextColor}
            variant={iconVariant}
            size={iconSize}
            prefixIcon={icon}
            onClick={iconOnClick}
            className={iconButtonClassName}
            testId="header-icon-button"
          />
        ) : null}
        {icon && !iconOnClick ? icon : null}
        <div
          className={clsx("text-text-primary", {
            "ml-4": (icon || iconOnClick) && inline,
            "mt-3": (icon || iconOnClick) && !inline,
            "mb-1": subtitle && !compact,
          })}
        >
          {eybrow ? <div className="text-200 text-text-primary-70">{eybrow}</div> : null}
          {title ? (
            <div className={titleSize} data-testid={testId}>
              {title}
            </div>
          ) : null}
          {subtitle ? (
            <div
              className={clsx(
                "text-text-primary-70",
                { "cursor-help": showSubtitleTooltip },
                subtitleClassName,
                subtitleSize,
              )}
              title={showSubtitleTooltip ? subtitle : undefined}
            >
              {subtitle}
            </div>
          ) : null}
          {description ? (
            <div className={clsx("mt-1 max-w-[600px] text-300 text-text-primary-70", descriptionClassName)}>
              {description}
            </div>
          ) : null}
        </div>
      </div>
      {children}
      {buttons ? (
        <div className={clsx("ml-3", { "phone:ml-0 phone:w-full": stackButtonsOnPhone }, buttonsWrapperClassName)}>
          <ButtonGroup buttons={buttons} variant={groupVariants.rightAligned} size={buttonSize} />
        </div>
      ) : null}
    </div>
  );
};

export default Header;
