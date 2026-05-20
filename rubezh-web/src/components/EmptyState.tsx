import type { LucideIcon } from "lucide-react";

interface Props {
  icon: LucideIcon;
  title: string;
  hint?: string;
  action?: React.ReactNode;
}

/** EmptyState — крупный иконочный блок для "нет данных" страниц. */
export function EmptyState({ icon: Icon, title, hint, action }: Props) {
  return (
    <div className="text-center py-12 px-4">
      <div className="inline-flex w-12 h-12 rounded-full bg-slate-800/60 items-center justify-center mb-3">
        <Icon className="w-6 h-6 text-slate-500" strokeWidth={1.5} />
      </div>
      <div className="text-slate-300 font-medium mb-1">{title}</div>
      {hint && (
        <div className="text-sm text-slate-500 max-w-md mx-auto">{hint}</div>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
