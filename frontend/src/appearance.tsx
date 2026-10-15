import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { CssBaseline, ThemeProvider } from '@mui/material';
import type { PaletteMode } from '@mui/material';
import type { ReactNode } from 'react';
import { createAppTheme } from './theme';

export type AppearanceMode = 'system' | 'light' | 'dark';

interface AppearanceContextValue {
  appearanceMode: AppearanceMode;
  resolvedMode: PaletteMode;
  setAppearanceMode: (mode: AppearanceMode) => void;
}

const APPEARANCE_STORAGE_KEY = 'lum-harbor-appearance';
const systemThemeQuery = '(prefers-color-scheme: dark)';
const AppearanceContext = createContext<AppearanceContextValue | null>(null);

function getStoredAppearance(): AppearanceMode {
  const stored = localStorage.getItem(APPEARANCE_STORAGE_KEY);
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system';
}

export function AppearanceProvider({ children }: { children: ReactNode }) {
  const [appearanceMode, setAppearanceMode] = useState<AppearanceMode>(getStoredAppearance);
  const [systemDark, setSystemDark] = useState(() => window.matchMedia(systemThemeQuery).matches);

  useEffect(() => {
    localStorage.setItem(APPEARANCE_STORAGE_KEY, appearanceMode);
  }, [appearanceMode]);

  useEffect(() => {
    const media = window.matchMedia(systemThemeQuery);
    const handleSystemThemeChange = (event: MediaQueryListEvent) => setSystemDark(event.matches);

    media.addEventListener('change', handleSystemThemeChange);
    return () => media.removeEventListener('change', handleSystemThemeChange);
  }, []);

  const resolvedMode: PaletteMode =
    appearanceMode === 'system' ? (systemDark ? 'dark' : 'light') : appearanceMode;
  const theme = useMemo(() => createAppTheme(resolvedMode), [resolvedMode]);
  const contextValue = useMemo(
    () => ({ appearanceMode, resolvedMode, setAppearanceMode }),
    [appearanceMode, resolvedMode],
  );

  return (
    <AppearanceContext.Provider value={contextValue}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        {children}
      </ThemeProvider>
    </AppearanceContext.Provider>
  );
}

export function useAppearance() {
  const context = useContext(AppearanceContext);
  if (!context) throw new Error('useAppearance must be used within AppearanceProvider');
  return context;
}
