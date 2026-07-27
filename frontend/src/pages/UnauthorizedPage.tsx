import { Link } from "react-router-dom";
import { EmptyState } from "../components/ui";
export const UnauthorizedPage = () => <EmptyState title="需要登录" detail="使用 GitHub 登录后才能访问后台管理。" action={<div className="flex justify-center gap-4"><a className="text-sm text-accent" href="/login">使用 GitHub 登录</a><Link className="text-sm text-muted" to="/">返回概览</Link></div>} />;
