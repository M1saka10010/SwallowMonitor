import { useEffect, useMemo, useState } from "react";
import { RouterProvider } from "react-router-dom";
import { api } from "../api";
import type { AuthState, SiteSettings } from "../api/types";
import { Button, PageState } from "../components/ui";
import { createAppRouter } from "../router";
import { AppContextProvider } from "./AppContext";
import { OverviewStreamProvider } from "../live/OverviewStreamProvider";

const defaultAuth: AuthState = { authEnabled: false, loggedIn: false, user: "" };
const defaultSettings: SiteSettings = { siteName: "SwallowMonitor", siteDescription: "" };

export function AppBootstrap() {
  const [auth, setAuth] = useState<AuthState>();
  const [settings, setSettings] = useState(defaultSettings);
  const [error, setError] = useState("");
  useEffect(() => { Promise.all([api.me(), api.settings()]).then(([nextAuth, nextSettings]) => { setAuth(nextAuth); setSettings(nextSettings); }).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "初始化失败")); }, []);
  useEffect(() => { document.title = settings.siteName; }, [settings.siteName]);
  const router = useMemo(() => createAppRouter(auth ?? defaultAuth), [auth]);
  if (error) return <PageState label="ERROR" tone="danger" message={`页面初始化失败：${error}`} action={<Button onClick={() => location.reload()}>重新加载</Button>} />;
  if (!auth) return <PageState message="正在读取监控状态…" />;
  return <AppContextProvider value={{ auth: auth ?? defaultAuth, settings, setSettings }}><OverviewStreamProvider><RouterProvider router={router} /></OverviewStreamProvider></AppContextProvider>;
}
