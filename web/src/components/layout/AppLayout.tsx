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
      <div className="flex h-screen w-screen overflow-hidden bg-background">
        <Sidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar />
          <main className="flex-1 overflow-y-auto">
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
