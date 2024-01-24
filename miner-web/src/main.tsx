import { createBrowserRouter, RouterProvider } from "react-router-dom";
import ReactDOM from "react-dom/client";

import Dashboard from "pages/Dashboard";
import Hardware from "pages/Hardware";
import Help from "pages/Help";
import Setup from "pages/Setup";

import App from "./App.tsx";

import "./index.css";

const router = createBrowserRouter([
  {
    path: "/",
    element: <App><Dashboard /></App>,
  },
  {
    path: "/dashboard",
    element: <App><Dashboard /></App>,
  },
  {
    path: "/hardware",
    element: <App><Hardware /></App>,
  },
  {
    path: "/setup",
    element: <App><Setup /></App>,
  },
  {
    path: "/help",
    element: <App><Help /></App>,
  },
]);

ReactDOM.createRoot(document.getElementById("root")!).render(<RouterProvider router={router} />);
