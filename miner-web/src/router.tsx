import { createBrowserRouter } from "react-router-dom";

import Hardware from "pages/Hardware";
import Home from "pages/Home";
import Onboarding from "pages/Onboarding";
import Settings from "pages/Settings";

import App from "./App.tsx";

const router = createBrowserRouter([
  {
    path: "/",
    element: <App><Home /></App>,
  },
  {
    path: "/hardware",
    element: <App><Hardware /></App>,
  },
  {
    path: "/onboarding",
    element: <Onboarding />,
  },
  {
    path: "/home",
    element: <App><Home /></App>,
  },
  {
    path: "/settings",
    element: <App><Settings /></App>,
  },
]);

export default router;
