/**
 * Shared framer-motion animation variants & transitions.
 * Used across all pages for consistent motion language.
 */
import type { Variants, Transition } from 'framer-motion';

/* ── Spring presets ── */
export const springGentle: Transition = {
  type: 'spring',
  stiffness: 200,
  damping: 30,
  mass: 0.8,
};

export const springSnappy: Transition = {
  type: 'spring',
  stiffness: 400,
  damping: 30,
  mass: 0.6,
};

export const springBouncy: Transition = {
  type: 'spring',
  stiffness: 300,
  damping: 20,
  mass: 0.8,
};

/* ── Fade variants ── */
export const fadeVariants: Variants = {
  hidden: { opacity: 0 },
  visible: { opacity: 1 },
  exit: { opacity: 0 },
};

export const fadeUpVariants: Variants = {
  hidden: { opacity: 0, y: 16 },
  visible: { opacity: 1, y: 0 },
  exit: { opacity: 0, y: 8 },
};

export const fadeScaleVariants: Variants = {
  hidden: { opacity: 0, scale: 0.96 },
  visible: { opacity: 1, scale: 1 },
  exit: { opacity: 0, scale: 0.96 },
};

export const slideLeftVariants: Variants = {
  hidden: { opacity: 0, x: -16 },
  visible: { opacity: 1, x: 0 },
  exit: { opacity: 0, x: -8 },
};

export const slideRightVariants: Variants = {
  hidden: { opacity: 0, x: 16 },
  visible: { opacity: 1, x: 0 },
  exit: { opacity: 0, x: 8 },
};

/* ── Stagger container ── */
export const staggerContainer: Variants = {
  hidden: {},
  visible: {
    transition: {
      staggerChildren: 0.05,
      delayChildren: 0.1,
    },
  },
};

export const staggerContainerSlow: Variants = {
  hidden: {},
  visible: {
    transition: {
      staggerChildren: 0.08,
      delayChildren: 0.15,
    },
  },
};

/* ── Nav item variants ── */
export const navItemVariants: Variants = {
  hidden: { opacity: 0, x: -12 },
  visible: { opacity: 1, x: 0, transition: springSnappy },
};

/* ── Card variants ── */
export const cardVariants: Variants = {
  hidden: { opacity: 0, y: 20, scale: 0.97 },
  visible: {
    opacity: 1,
    y: 0,
    scale: 1,
    transition: springGentle,
  },
  exit: { opacity: 0, y: 10, scale: 0.98 },
};

/* ── Hover / tap gestures ── */
export const hoverScale = {
  whileHover: { scale: 1.02 },
  whileTap: { scale: 0.98 },
  transition: springSnappy,
};

export const hoverLift = {
  whileHover: { y: -2, boxShadow: '0 8px 32px rgba(0,0,0,0.15)' },
  whileTap: { y: 0 },
  transition: springGentle,
};
