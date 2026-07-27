import { Link } from "react-router-dom";
import { EmptyState } from "../components/ui";
export const NotFoundPage = () => <EmptyState title="页面不存在" detail="该地址无法匹配 SwallowMonitor 中的页面。" action={<Link className="text-sm text-accent" to="/">返回概览</Link>} />;
