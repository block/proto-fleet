import { ReactNode } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";
import ButtonGroup, {
  ButtonProps,
  groupVariants,
} from "@/shared/components/ButtonGroup";

interface HeaderProps {
  buttons?: ButtonProps[];
  buttonSize?: keyof typeof sizes;
  centerButton?: boolean;
  className?: string;
  compact?: boolean;
  icon?: ReactNode;
  iconOnClick?: () => void;
  inline?: boolean;
  showSubtitleTooltip?: boolean;
  subtitle?: string;
  subtitleClassName?: string;
  subtitleSize?: string;
  testId?: string;
  title?: string;
  titleSize?: string;
  eybrow?: string;
  description?: string;
}

const Header = ({
  buttons,
  buttonSize = sizes.compact,
  centerButton,
  className,
  compact,
  icon,
  iconOnClick,
  inline = false,
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
        "flex w-full justify-between",
        { "items-center": centerButton },
        className,
      )}
    >
      <div className={clsx("w-full", { "flex items-start": inline })}>
        {icon && iconOnClick && (
          <Button
            variant={variants.secondary}
            size={sizes.base}
            prefixIcon={icon}
            onClick={iconOnClick}
            testId="header-icon-button"
          />
        )}
        {icon && !iconOnClick && icon}
        <div
          className={clsx("text-text-primary", {
            "ml-4": (icon || iconOnClick) && inline,
            "mt-3": (icon || iconOnClick) && !inline,
            "mb-1": subtitle && !compact,
          })}
        >
          {eybrow && (
            <div className="text-200 text-text-primary-70">{eybrow}</div>
          )}
          {title && (
            <div className={titleSize} data-testid={testId}>
              {title}
            </div>
          )}
          {subtitle && (
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
          )}
          {description && (
            <div className="text-300 text-text-primary-70">{description}</div>
          )}
        </div>
      </div>
      {buttons && (
        <div className="ml-3">
          <ButtonGroup
            buttons={buttons}
            variant={groupVariants.rightAligned}
            size={buttonSize}
          />
        </div>
      )}
    </div>
  );
};

export default Header;
