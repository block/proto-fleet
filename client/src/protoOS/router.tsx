import { createBrowserRouter, Outlet, redirect } from "react-router-dom";

import App from "@/protoOS/components/App";
import FullScreenContentLayout from "@/protoOS/components/ContentLayout/FullScreenContentLayout";
import {
  Efficiency,
  HashboardTemperature,
  Hashrate,
  KpiLayout,
  PowerUsage,
  Temperature,
} from "@/protoOS/features/kpis";
import {
  Cooling,
  General,
  Hardware,
  MiningPools,
} from "@/protoOS/features/settings";
import Logs from "@/protoOS/pages/MinerLogs";
import Onboarding from "@/protoOS/pages/Onboarding";
import AuthenticationPage from "@/protoOS/pages/Onboarding/Authentication";
import MiningPoolPage from "@/protoOS/pages/Onboarding/MiningPool/MiningPool";
import NetworkPage from "@/protoOS/pages/Onboarding/Network";
import Verify from "@/protoOS/pages/Onboarding/Verify";
import Welcome from "@/protoOS/pages/Onboarding/Welcome";

export const routerConfig = [
  {
    path: "",
    element: <App title="Home" ContentLayout={KpiLayout} />,
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
    element: <NetworkPage />,
  },
  {
    path: "onboarding/authentication",
    element: <AuthenticationPage />,
  },
  {
    path: "onboarding/mining-pool",
    element: <MiningPoolPage />,
  },
  {
    path: "settings",
    // TODO: look into modifying App to use Outlet instead of children
    element: (
      <App title="Settings">
        <Outlet />
      </App>
    ),
    children: [
      {
        index: true,
        loader: () => redirect("general"),
      },
      {
        path: "general",
        element: <General />,
      },
      {
        path: "mining-pools",
        element: <MiningPools />,
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

const router = createBrowserRouter(routerConfig);

export default router;
