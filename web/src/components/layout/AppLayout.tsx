import { Outlet, useLocation } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { TooltipProvider } from '@/components/ui/tooltip';
import Sidebar from './Sidebar';
import TopBar from './TopBar';
import { fadeVariants, springGentle } from '@/lib/motion';

export default function AppLayout() {
  const location = useLocation();

  return (
    <TooltipProvider delayDuration={200}>
      <div className="relative flex h-screen w-screen overflow-hidden bg-background">
        {/* Global atmospheric layers — interactive art canvas */}
        <div className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden>
          <div className="absolute -left-[20%] top-[-10%] h-[55vh] w-[55vh] rounded-full bg-primary/10 blur-[100px]" />
          <div className="absolute -right-[15%] top-[20%] h-[40vh] w-[40vh] rounded-full bg-indigo-500/10 blur-[110px]" />
          <div className="absolute bottom-[-20%] left-[30%] h-[45vh] w-[45vh] rounded-full bg-emerald-500/5 blur-[120px]" />
          <div
            className="absolute inset-0 opacity-[0.035]"
            style={{
              backgroundImage:
                'linear-gradient(hsl(var(--foreground)/0.06) 1px, transparent 1px), linear-gradient(90deg, hsl(var(--foreground)/0.06) 1px, transparent 1px)',
              backgroundSize: '64px 64px',
              maskImage: 'radial-gradient(ellipse at center, black 20%, transparent 75%)',
            }}
          />
        </div>

        <Sidebar />
        <div className="relative flex min-w-0 flex-1 flex-col">
          <TopBar />
          <main className="mesh-canvas flex-1 overflow-y-auto">
            <AnimatePresence mode="wait">
              <motion.div
                key={location.pathname}
                variants={fadeVariants}
                initial="hidden"
                animate="visible"
                exit="exit"
                transition={springGentle}
                className="h-full"
              >
                <Outlet />
              </motion.div>
            </AnimatePresence>
          </main>
        </div>
      </div>
    </TooltipProvider>
  );
}
