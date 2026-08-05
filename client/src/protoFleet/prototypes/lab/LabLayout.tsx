/** Minimal chrome for the Prototype Lab — a header + back link + <Outlet />. */
import { Link, Outlet, useLocation } from "react-router-dom";

const TABS = [
  { path: "/lab", label: "Overview", exact: true },
  { path: "/lab/fleet-native", label: "1 · Fleet-native" },
  { path: "/lab/proxy", label: "2 · Proxy (versioned)" },
  { path: "/lab/adapter", label: "3 · Adapter" },
];

export default function LabLayout() {
  const { pathname } = useLocation();
  return (
    <div className="min-h-screen bg-surface-base text-text-primary">
      <div className="mx-auto flex max-w-4xl flex-col gap-6 p-6">
        <header className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <span className="bg-emphasis-300 rounded px-2 py-0.5 text-heading-100 tracking-wide text-surface-base uppercase">
              Prototype
            </span>
            <h1 className="text-heading-200">Single-miner view · The Lab</h1>
          </div>
          <nav className="flex flex-wrap gap-2">
            {TABS.map((tab) => {
              const active = tab.exact ? pathname === tab.path : pathname.startsWith(tab.path);
              return (
                <Link
                  key={tab.path}
                  to={tab.path}
                  className={`rounded-md px-3 py-1.5 text-200 ${
                    active
                      ? "bg-surface-elevated-base text-text-primary"
                      : "text-text-primary-50 hover:text-text-primary"
                  }`}
                >
                  {tab.label}
                </Link>
              );
            })}
          </nav>
        </header>
        <main>
          <Outlet />
        </main>
      </div>
    </div>
  );
}
