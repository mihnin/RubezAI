/** Skeleton — shimmer-loader блок. Tailwind animate-pulse + slate-800. */
export function Skeleton({ className = "" }: { className?: string }) {
  return (
    <div
      className={`animate-pulse bg-slate-800/60 rounded ${className}`}
      aria-hidden="true"
    />
  );
}

/** Несколько строк подряд (для табличной/листовой загрузки). */
export function SkeletonRows({
  count = 3,
  className = "h-12",
}: {
  count?: number;
  className?: string;
}) {
  return (
    <div className="space-y-2">
      {Array.from({ length: count }, (_, i) => (
        <Skeleton key={i} className={className} />
      ))}
    </div>
  );
}
