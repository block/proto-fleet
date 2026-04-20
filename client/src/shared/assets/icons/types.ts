interface BaseIconProps {
  ariaExpanded?: boolean;
  className?: string;
  opacity?: string;
  testId?: string;
  width?: string;
}

type StaticIconProps = BaseIconProps & {
  ariaLabel?: string;
  onClick?: undefined;
};

type InteractiveIconProps = BaseIconProps & {
  ariaLabel: string;
  onClick: () => void;
};

export type IconProps = StaticIconProps | InteractiveIconProps;
