import { Fragment } from "react";

import {
  Alert,
  ArrowLeftCompact,
  ArrowRight,
  BankAccount,
  Bitcoin,
  Checkmark,
  ChevronDown,
  ConcentricCircles,
  Curtail,
  Dismiss,
  DismissCircle,
  DismissCircleDark,
  DismissTiny,
  Download,
  Ellipsis,
  Eye,
  Fan,
  Fleet,
  Globe,
  Graph,
  Grip,
  HashboardIndicator,
  Home,
  Info,
  InfoInverted,
  LEDIndicator,
  Lightning,
  Lock,
  Logo,
  LogoAlt,
  Logs,
  Minus,
  Notification,
  Pause,
  Plus,
  Power,
  Question,
  Reboot,
  Rectangle,
  Repair,
  Settings,
  Speedometer,
  Stop,
  Success,
  Terminal,
  ThemeDark,
  ThemeLight,
  ThemeSystem,
} from ".";

const icons = {
  Alert,
  ArrowLeftCompact,
  ArrowRight,
  BankAccount,
  Bitcoin,
  Checkmark,
  ChevronDown,
  ConcentricCircles,
  Curtail,
  Dismiss,
  DismissCircle,
  DismissCircleDark,
  DismissTiny,
  Download,
  Ellipsis,
  Eye,
  Fan,
  Fleet,
  Globe,
  Graph,
  Grip,
  HashboardIndicator,
  Home,
  Info,
  InfoInverted,
  LEDIndicator,
  Lightning,
  Lock,
  Logo,
  LogoAlt,
  Logs,
  Minus,
  Notification,
  Pause,
  Plus,
  Power,
  Question,
  Reboot,
  Rectangle,
  Repair,
  Settings,
  Speedometer,
  Stop,
  Success,
  Terminal,
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
              <td className="border border-border-5 border-r-transparent p-4">
                {icon}
              </td>
              <td className="m-auto border border-border-5 border-l-transparent">
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
