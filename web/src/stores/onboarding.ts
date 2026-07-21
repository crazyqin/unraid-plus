import { create } from 'zustand';
import { persist } from 'zustand/middleware';

/**
 * Tracks the new-user onboarding wizard step and the user's known skill level,
 * used to decide how verbose the helper copy should be.
 */
type Skill = 'novice' | 'intermediate' | 'expert';

interface OnboardingState {
  step: number;
  skill: Skill;
  setStep: (n: number) => void;
  next: () => void;
  prev: () => void;
  reset: () => void;
  setSkill: (s: Skill) => void;
}

export const useOnboardingStore = create<OnboardingState>()(
  persist(
    (set) => ({
      step: 0,
      skill: 'novice',
      setStep: (n) => set({ step: n }),
      next: () => set((s) => ({ step: s.step + 1 })),
      prev: () => set((s) => ({ step: Math.max(0, s.step - 1) })),
      reset: () => set({ step: 0 }),
      setSkill: (s) => set({ skill: s }),
    }),
    { name: 'unraidpp-onboarding' },
  ),
);
