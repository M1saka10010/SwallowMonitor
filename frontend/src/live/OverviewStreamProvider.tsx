import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import type { LiveEvent } from "../api/types";
import { parseLiveEvent } from "./events";

type Listener = (event: LiveEvent) => void;
interface StreamContextValue { subscribe: (listener: Listener) => () => void; connection: "connecting" | "open" | "reconnecting"; generation: number }
const StreamContext = createContext<StreamContextValue | null>(null);

export function OverviewStreamProvider({ children }: { children: ReactNode }) {
  const listeners = useRef(new Set<Listener>());
  const [connection, setConnection] = useState<StreamContextValue["connection"]>("connecting");
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    const source = new EventSource("/events");
    let opened = false;
    source.onopen = () => { setConnection("open"); if (opened) setGeneration((value) => value + 1); opened = true; };
    source.onerror = () => setConnection("reconnecting");
    source.onmessage = (message) => {
      const event = parseLiveEvent(message.data);
      if (event) listeners.current.forEach((listener) => listener(event));
    };
    return () => source.close();
  }, []);
  const subscribe = (listener: Listener) => { listeners.current.add(listener); return () => listeners.current.delete(listener); };
  return <StreamContext.Provider value={{ subscribe, connection, generation }}>{children}</StreamContext.Provider>;
}

export function useOverviewStream() { const value = useContext(StreamContext); if (!value) throw new Error("OverviewStreamProvider missing"); return value; }
