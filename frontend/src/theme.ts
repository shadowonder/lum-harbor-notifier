import { alpha, createTheme } from '@mui/material/styles';
import type { PaletteMode } from '@mui/material';

declare module '@mui/material/styles' {
  interface Palette {
    surface: { sidebar: string; secondary: string; subtle: string };
    attention: { border: string; controlBorder: string };
    icon: { muted: string };
  }

  interface PaletteOptions {
    surface?: { sidebar: string; secondary: string; subtle: string };
    attention?: { border: string; controlBorder: string };
    icon?: { muted: string };
  }
}

const systemFont = ['-apple-system', 'BlinkMacSystemFont', '"Segoe UI"', 'sans-serif'].join(',');

export function createAppTheme(mode: PaletteMode) {
  const dark = mode === 'dark';

  return createTheme({
    palette: {
      mode,
      primary: dark
        ? { main: '#7FA4CA', dark: '#B2CCE4', light: '#2A3B4D' }
        : { main: '#466B91', dark: '#345574', light: '#E3EDF6' },
      warning: dark ? { main: '#C69358', light: '#2A241D' } : { main: '#A2682A', light: '#FBF4E9' },
      error: { main: dark ? '#D28075' : '#A85147' },
      success: dark ? { main: '#83AA90', light: '#233029' } : { main: '#52705E', light: '#EDF3EF' },
      background: dark
        ? { default: '#181B1F', paper: '#20242A' }
        : { default: '#F5F6F7', paper: '#FFFFFF' },
      text: dark
        ? { primary: '#E6E9EC', secondary: '#A7B0BA' }
        : { primary: '#252B31', secondary: '#5F6974' },
      divider: dark ? '#3A414A' : '#D7DCE1',
      surface: dark
        ? { sidebar: '#1C2025', secondary: '#2C3239', subtle: '#1E2329' }
        : { sidebar: '#F0F2F4', secondary: '#ECF1F5', subtle: '#F8F9FA' },
      attention: dark
        ? { border: '#594733', controlBorder: '#6A5338' }
        : { border: '#E7D8C3', controlBorder: '#DCCBB3' },
      icon: { muted: dark ? '#8F99A5' : '#85919D' },
    },
    typography: {
      fontFamily: systemFont,
      h1: { fontSize: '1.75rem', lineHeight: 1.25, fontWeight: 650, letterSpacing: '-0.035em' },
      h2: { fontSize: '0.875rem', lineHeight: 1.4, fontWeight: 650 },
      h3: { fontSize: '0.875rem', lineHeight: 1.4, fontWeight: 650 },
      body1: { fontSize: '0.8125rem', lineHeight: 1.55 },
      body2: { fontSize: '0.75rem', lineHeight: 1.55 },
      button: { fontSize: '0.75rem', fontWeight: 600, textTransform: 'none' },
      caption: { fontSize: '0.6875rem', lineHeight: 1.45 },
    },
    shape: { borderRadius: 8 },
    breakpoints: { values: { xs: 0, sm: 600, md: 900, lg: 1280, xl: 1536 } },
    components: {
      MuiCssBaseline: {
        styleOverrides: {
          html: { height: '100%', colorScheme: mode },
          body: { height: '100%', overflow: 'hidden' },
          '#root': { height: '100%' },
          '*': { boxSizing: 'border-box' },
        },
      },
      MuiPaper: {
        styleOverrides: {
          root: { backgroundImage: 'none' },
          outlined: {
            borderColor: dark ? '#3A414A' : '#D7DCE1',
            boxShadow: `0 1px 2px ${alpha(dark ? '#000000' : '#18212B', dark ? 0.16 : 0.025)}`,
          },
        },
      },
      MuiButton: {
        defaultProps: { disableElevation: true },
        styleOverrides: { root: { borderRadius: 6, minHeight: 32 } },
      },
      MuiTooltip: {
        defaultProps: { arrow: true, enterDelay: 450 },
      },
    },
  });
}
