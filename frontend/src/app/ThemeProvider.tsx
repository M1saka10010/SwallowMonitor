import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import type { ThemePreference } from "../api/types";

interface ThemeContextValue { theme: ThemePreference; setTheme: (theme: ThemePreference) => void; revision: number }
const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemePreference>(() => {
    const saved = localStorage.getItem("theme");
    return saved === "light" || saved === "dark" ? saved : "auto";
  });
  const [revision, setRevision] = useState(0);
  const setTheme = (next: ThemePreference) => {
    setThemeState(next);
    if (next === "auto") localStorage.removeItem("theme"); else localStorage.setItem("theme", next);
  };
  useEffect(() => {
    if (theme === "auto") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = theme;
  }, [theme]);
  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const update = () => { if (theme === "auto") setRevision((value) => value + 1); };
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [theme]);
  const value = useMemo(() => ({ theme, setTheme, revision }), [theme, revision]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("ThemeProvider missing");
  return value;
}
