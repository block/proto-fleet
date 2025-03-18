import { Fragment } from "react";

import {
  Alert,
  ArrowRight,
  Checkmark,
  ChevronDown,
  ConcentricCircles,
  Dismiss,
  DismissCircle,
  DismissCircleDark,
  DismissTiny,
  Ellipsis,
  Eye,
  Fleet,
  Graph,
  Grip,
  HashboardIndicator,
  Home,
  Info,
  InfoInverted,
  Logo,
  LogoAlt,
  Logs,
  Minus,
  Pause,
  Plus,
  Power,
  Question,
  Repair,
  Settings,
  Stop,
  Success,
  ThemeDark,
  ThemeLight,
  ThemeSystem,
} from ".";

const icons = {
  Alert,
  ArrowRight,
  Checkmark,
  ChevronDown,
  ConcentricCircles,
  Dismiss,
  DismissCircle,
  DismissCircleDark,
  DismissTiny,
  Ellipsis,
  Eye,
  Fleet,
  Graph,
  Grip,
  HashboardIndicator,
  Home,
  Info,
  InfoInverted,
  Logo,
  LogoAlt,
  Logs,
  Minus,
  Pause,
  Plus,
  Power,
  Question,
  Repair,
  Settings,
  Stop,
  Success,
  ThemeDark,
  ThemeLight,
  ThemeSystem,
};

interface IconProps {
  Icons: (keyof typeof icons)[];
}

const IconWrapper = ({ Icons }: IconProps) => {
  const loopCount = Math.ceil(Icons.length / 5);
  return [...Array(loopCount)].map((_, index) => {
    const start = index * 5;
    const end = start + 5;
    const slicedIcons = Icons.slice(start, end);
    return (
      <tr key={index}>
        {slicedIcons.map((icon: keyof typeof icons) => {
          const Icon = icons[icon];
          return (
            <Fragment key={icon}>
              <td className="p-4 border border-r-transparent border-border-5">
                {icon}
              </td>
              <td className="border border-l-transparent border-border-5 m-auto">
                <div className="flex justify-center">
                  <Icon />
                </div>
              </td>
            </Fragment>
          );
        })}
      </tr>
    );
  });
};

export const Icons = () => {
  return (
    <table className="w-full table-fixed">
      <tbody>
        <IconWrapper Icons={Object.keys(icons) as (keyof typeof icons)[]} />
      </tbody>
    </table>
  );
};

export default {
  title: "Style/Icons",
};
