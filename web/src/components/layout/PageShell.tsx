import type { ReactNode } from 'react';
import { motion } from 'framer-motion';
import { cn } from '@/lib/utils';
import { springGentle } from '@/lib/motion';

/**
 * Shared page chrome — same padding/spacing on Dashboard, Storage, Docker, VMs, Settings…
 * Avoids one page hugging the top-left while others sit lower.
 */
export function PageShell({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn('relative min-h-full space-y-6 p-5 md:p-8', className)}>
      {children}
    </div>
  );
}

/** Soft ambient orb behind page content (optional decorative layer). */
export function PageOrb({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        'pointer-events-none absolute h-72 w-72 rounded-full blur-3xl',
        className,
      )}
      aria-hidden
    />
  );
}

/**
 * Unified page title block:
 *   optional eyebrow (small caps)
 *   large title
 *   optional meta row (badges / status)
 *   optional right-side actions
 */
export function PageHeader({
  eyebrow,
  title,
  titleClassName,
  meta,
  actions,
  className,
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  titleClassName?: string;
  meta?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <motion.div
      className={cn(
        'flex flex-wrap items-end justify-between gap-4',
        className,
      )}
      initial={{ opacity: 0, y: 14 }}
      animate={{ opacity: 1, y: 0 }}
      transition={springGentle}
    >
      <div className="min-w-0 space-y-2.5">
        {eyebrow != null && eyebrow !== false && (
          <div className="text-[10px] font-semibold uppercase tracking-[0.28em] text-muted-foreground/65">
            {eyebrow}
          </div>
        )}
        <h1
          className={cn(
            'text-display-md tracking-tight text-foreground',
            titleClassName,
          )}
        >
          {title}
        </h1>
        {meta != null && (
          <div className="flex flex-wrap items-center gap-2 pt-0.5 text-sm text-muted-foreground">
            {meta}
          </div>
        )}
      </div>
      {actions != null && (
        <div className="flex flex-wrap items-center gap-2">{actions}</div>
      )}
    </motion.div>
  );
}
