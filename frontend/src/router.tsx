import { createHashRouter } from "react-router-dom";
import { AppShell } from "./app/AppShell";
import { Button, PageState } from "./components/ui";
import { NotFoundPage } from "./pages/NotFoundPage";
import { OverviewPage } from "./pages/overview/OverviewPage";
import { UnauthorizedPage } from "./pages/UnauthorizedPage";

export function createAppRouter() {
  return createHashRouter([{ path: "/", element: <AppShell />, hydrateFallbackElement: <PageState message="正在加载页面…" />, errorElement: <PageState label="ERROR" tone="danger" message="页面加载失败，请重新加载。" action={<Button onClick={() => location.reload()}>重新加载</Button>} />, children: [
    { index: true, element: <OverviewPage /> },
    { path: "hosts/:publicId", lazy: async () => ({ Component: (await import("./pages/host/HostDetailPage")).HostDetailPage }) },
    { path: "unauthorized", element: <UnauthorizedPage /> },
    { path: "admin", lazy: async () => ({ Component: (await import("./pages/admin/AdminPage")).AdminPage }), children: [
      { index: true, lazy: async () => ({ Component: (await import("./pages/admin/AdminPage")).AdminIndex }) },
      { path: "hosts", lazy: async () => ({ Component: (await import("./pages/admin/AdminPage")).HostsPanel }) },
      { path: "tags", lazy: async () => ({ Component: (await import("./pages/admin/AdminPage")).TagsPanel }) },
      { path: "settings", lazy: async () => ({ Component: (await import("./pages/admin/AdminPage")).SettingsPanel }) },
      { path: "notifications", lazy: async () => ({ Component: (await import("./pages/admin/AdminPage")).NotificationsPanel }) },
    ] },
    { path: "*", element: <NotFoundPage /> },
  ] }]);
}
