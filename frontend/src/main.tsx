import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AppBootstrap } from "./app/AppBootstrap";
import { ThemeProvider } from "./app/ThemeProvider";
import { ErrorBoundary } from "./app/ErrorBoundary";
import "./styles.css";

createRoot(document.getElementById("root")!).render(<StrictMode><ErrorBoundary><ThemeProvider><AppBootstrap /></ThemeProvider></ErrorBoundary></StrictMode>);
