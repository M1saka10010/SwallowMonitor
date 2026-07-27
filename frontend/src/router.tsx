import { createHashRouter } from "react-router-dom";
import type { AuthState } from "./api/types";
import { AppShell } from "./app/AppShell";
import { AdminIndex, AdminPage, HostsPanel, NotificationsPanel, SettingsPanel, TagsPanel } from "./pages/admin/AdminPage";
import { HostDetailPage } from "./pages/host/HostDetailPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { OverviewPage } from "./pages/overview/OverviewPage";
import { UnauthorizedPage } from "./pages/UnauthorizedPage";

export function createAppRouter(_auth: AuthState) {
  return createHashRouter([{ path: "/", element: <AppShell />, children: [
    { index: true, element: <OverviewPage /> },
    { path: "hosts/:publicId", element: <HostDetailPage /> },
    { path: "unauthorized", element: <UnauthorizedPage /> },
    { path: "admin", element: <AdminPage />, children: [{ index: true, element: <AdminIndex /> }, { path: "hosts", element: <HostsPanel /> }, { path: "tags", element: <TagsPanel /> }, { path: "settings", element: <SettingsPanel /> }, { path: "notifications", element: <NotificationsPanel /> }] },
    { path: "*", element: <NotFoundPage /> },
  ] }]);
}
