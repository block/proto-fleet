import { createBrowserRouter } from "react-router-dom";

import Hardware from "pages/Hardware";
import Help from "pages/Help";
import Onboarding from "pages/Onboarding";
import Performance from "pages/Performance";
import Settings from "pages/Settings";

import App from "./App.tsx";

const router = createBrowserRouter([
  {
    path: "/",
    element: <App><Performance /></App>,
  },
  {
    path: "/hardware",
    element: <App><Hardware /></App>,
  },
  {
    path: "/help",
    element: <App><Help /></App>,
  },
  {
    path: "/onboarding",
    element: <Onboarding />,
  },
  {
    path: "/performance",
    element: <App><Performance /></App>,
  },
  {
    path: "/settings",
    element: <App><Settings /></App>,
  },
]);

export default router;
