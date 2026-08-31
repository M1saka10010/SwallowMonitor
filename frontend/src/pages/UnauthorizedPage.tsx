import { Link } from "react-router-dom";
import { EmptyState } from "../components/ui";
export const UnauthorizedPage = () => <EmptyState title="需要登录" detail="使用 GitHub 登录后才能访问后台管理。" action={<div className="flex justify-center gap-6"><a className="text-sm text-ink underline underline-offset-4" href="/login">使用 GitHub 登录</a><Link className="text-sm text-muted transition-colors hover:text-ink" to="/">返回概览</Link></div>} />;
