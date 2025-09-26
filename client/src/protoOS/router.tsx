import { createBrowserRouter, Outlet, redirect } from "react-router-dom";

import App from "@/protoOS/components/App";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import SettingsContentLayout from "@/protoOS/components/ContentLayout/SettingsContentLayout";
import {
  Efficiency,
  HashboardTemperature,
  Hashrate,
  KpiLayout,
  PowerUsage,
  Temperature,
} from "@/protoOS/features/kpis";
import {
  Authentication,
  MiningPool,
  Network,
  Onboarding,
  Verify,
  Welcome,
} from "@/protoOS/features/onboarding";
import {
  Authentication as AuthenticationSettings,
  Cooling,
  General,
  Hardware,
  MiningPools,
} from "@/protoOS/features/settings";
import Logs from "@/protoOS/pages/MinerLogs";

export const routerConfig = [
  {
    path: "",
    element: <App title="Home" ContentLayout={KpiLayout} />,
    requiresAuth: false,
    children: [
      {
        index: true,
        loader: () => redirect("hashrate"),
      },
      {
        path: "hashrate",
        element: <Hashrate />,
      },
      {
        path: "efficiency",
        element: <Efficiency />,
      },
      {
        path: "power-usage",
        element: <PowerUsage />,
      },
      {
        path: "temperature",
        element: <Temperature />,
      },
    ],
  },
  {
    path: "temperature/:serial",
    element: <HashboardTemperature />,
  },
  {
    path: "logs",
    element: (
      <App title="Logs" hideErrors ContentLayout={FullScreenContentLayout}>
        <Logs />
      </App>
    ),
  },
  {
    path: "onboarding",
    element: <Onboarding />,
  },
  {
    path: "onboarding/welcome",
    element: <Welcome />,
  },
  {
    path: "onboarding/verify",
    element: <Verify />,
  },
  {
    path: "onboarding/network",
    element: <Network />,
  },
  {
    path: "onboarding/authentication",
    element: <Authentication />,
  },
  {
    path: "onboarding/mining-pool",
    element: <MiningPool />,
  },
  {
    path: "settings",
    element: (
      <App title="Settings" ContentLayout={SettingsContentLayout}>
        <Outlet />
      </App>
    ),
    children: [
      {
        index: true,
        loader: () => redirect("general"),
      },
      {
        path: "authentication",
        element: <AuthenticationSettings />,
      },
      {
        path: "general",
        element: <General />,
      },
      {
        path: "mining-pools",
        element: <MiningPools />,
        requiresAuth: true,
      },
      {
        path: "hardware",
        element: <Hardware />,
      },
      {
        path: "cooling",
        element: <Cooling />,
      },
    ],
  },
];

export const createRouter = () => createBrowserRouter(routerConfig);
