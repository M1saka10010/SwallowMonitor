import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props { children: ReactNode }
interface State { failed: boolean }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false };

  static getDerivedStateFromError(): State { return { failed: true }; }

  componentDidCatch(error: Error, info: ErrorInfo) {
    if (import.meta.env.DEV) console.error("React render failed", error, info.componentStack);
  }

  render() {
    if (this.state.failed) {
      return <main className="mx-auto max-w-xl px-4 py-24 text-center"><h1 className="text-lg font-semibold">页面渲染失败</h1><p className="mt-2 text-sm text-muted">重新加载页面。如果问题持续，请检查浏览器控制台和服务日志。</p><button type="button" className="mt-5 min-h-11 rounded border border-line bg-surface px-4 text-sm" onClick={() => location.reload()}>重新加载</button></main>;
    }
    return this.props.children;
  }
}
