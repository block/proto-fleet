import { type ReactNode } from "react";
import { Alert, Building, Fan, Globe, Hashboard, Power, Repair } from "@/shared/assets/icons";

export const getComponentIcon = (component: string, urgent: boolean): ReactNode => {
  if (urgent) return <Alert />;

  switch (component) {
    case "Fan":
      return <Fan />;
    case "Hashboard":
      return <Hashboard />;
    case "PSU":
    case "Electrical":
      return <Power />;
    case "Control Board":
      return <Repair />;
    case "Network":
      return <Globe />;
    case "Building":
    case "Cleaning":
      return <Building />;
    case "HVAC":
      return <Fan />;
    default:
      return <Repair />;
  }
};

export const getComponentIconColor = (urgent: boolean): string =>
  urgent ? "text-text-critical" : "text-text-primary-70";
