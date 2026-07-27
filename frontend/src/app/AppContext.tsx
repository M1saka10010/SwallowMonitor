import { createContext, useContext, type ReactNode } from "react";
import type { AuthState, SiteSettings } from "../api/types";

export interface AppContextValue {
  auth: AuthState;
  settings: SiteSettings;
  setSettings: (settings: SiteSettings) => void;
}
const AppContext = createContext<AppContextValue | null>(null);
export const AppContextProvider = ({ value, children }: { value: AppContextValue; children: ReactNode }) => <AppContext.Provider value={value}>{children}</AppContext.Provider>;
export function useApp() { const value = useContext(AppContext); if (!value) throw new Error("AppContext missing"); return value; }
