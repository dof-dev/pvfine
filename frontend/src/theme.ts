import {
  darkTheme,
  lightTheme,
  type GlobalThemeOverrides,
} from "naive-ui";

export type ThemeMode = "dark" | "light" | "system";
export type ResolvedThemeId = "dark" | "light";

interface StatusPalette {
  text: string;
  hover: string;
  pressed: string;
  surface: string;
}

export interface ThemePalette {
  window: {
    solidBackground: string;
    glassBackground: string;
  };
  surface: {
    panel: string;
    card: string;
    elevated: string;
    subtle: string;
    list: string;
    hover: string;
    selected: string;
    dragPreview: string;
    inset: string;
    code: string;
    warning: string;
    success: string;
    error: string;
  };
  text: {
    primary: string;
    secondary: string;
    tertiary: string;
    muted: string;
    faint: string;
    disabled: string;
    onAccent: string;
    code: string;
  };
  border: {
    faint: string;
    subtle: string;
    normal: string;
    strong: string;
  };
  primary: {
    base: string;
    hover: string;
    pressed: string;
    soft: string;
    selected: string;
  };
  status: {
    info: StatusPalette;
    success: StatusPalette;
    warning: StatusPalette;
    error: StatusPalette;
  };
  editor: {
    gutterText: string;
    activeLine: string;
    selectionMatch: string;
    annotationText: string;
    annotationSurface: string;
    annotationBorder: string;
    annotationEnumText: string;
    annotationEnumSurface: string;
    annotationEnumBorder: string;
    annotationReferenceText: string;
    annotationReferenceSurface: string;
    annotationReferenceBorder: string;
    annotationLink: string;
    syntaxNumber: string;
    syntaxString: string;
    syntaxHeading: string;
  };
  effects: {
    scrollbar: string;
    scrollbarHover: string;
    tooltipShadow: string;
    successGlow: string;
    focusRing: string;
    activePaneRing: string;
    splitHover: string;
    successRing: string;
    successEdge: string;
  };
}

export interface ThemeDefinition {
  id: ResolvedThemeId;
  label: string;
  palette: ThemePalette;
  naiveTheme: typeof darkTheme | typeof lightTheme;
}

const darkPalette: ThemePalette = {
  window: {
    solidBackground: "#181a20",
    glassBackground: "rgba(24, 26, 32, 0.68)",
  },
  surface: {
    panel: "rgba(30, 33, 41, 0.55)",
    card: "#1e2129",
    elevated: "#242832",
    subtle: "rgba(255, 255, 255, 0.018)",
    list: "rgba(255, 255, 255, 0.025)",
    hover: "rgba(79, 140, 255, 0.09)",
    selected: "rgba(79, 140, 255, 0.15)",
    dragPreview: "rgba(79, 140, 255, 0.22)",
    inset: "rgba(128, 128, 128, 0.035)",
    code: "rgba(0, 0, 0, 0.18)",
    warning: "rgba(230, 180, 100, 0.08)",
    success: "rgba(50, 160, 100, 0.12)",
    error: "rgba(210, 70, 70, 0.12)",
  },
  text: {
    primary: "rgba(255, 255, 255, 0.92)",
    secondary: "rgba(255, 255, 255, 0.78)",
    tertiary: "rgba(235, 238, 245, 0.95)",
    muted: "rgba(255, 255, 255, 0.5)",
    faint: "rgba(255, 255, 255, 0.42)",
    disabled: "rgba(128, 128, 128, 0.85)",
    onAccent: "#ffffff",
    code: "#c9d7ed",
  },
  border: {
    faint: "rgba(128, 128, 128, 0.12)",
    subtle: "rgba(128, 128, 128, 0.16)",
    normal: "rgba(128, 128, 128, 0.2)",
    strong: "rgba(128, 128, 128, 0.22)",
  },
  primary: {
    base: "#4f8cff",
    hover: "#6ba0ff",
    pressed: "#3a75e8",
    soft: "rgba(79, 140, 255, 0.1)",
    selected: "rgba(79, 140, 255, 0.15)",
  },
  status: {
    info: {
      text: "#8ab4ff",
      hover: "#a5c6ff",
      pressed: "#6d9cff",
      surface: "rgba(79, 140, 255, 0.09)",
    },
    success: {
      text: "#63e2b7",
      hover: "#7fe7c4",
      pressed: "#5acea7",
      surface: "rgba(50, 160, 100, 0.12)",
    },
    warning: {
      text: "#f2c97d",
      hover: "#ffdc9e",
      pressed: "#e8b464",
      surface: "rgba(242, 201, 125, 0.08)",
    },
    error: {
      text: "#e88080",
      hover: "#ff9b9b",
      pressed: "#d96868",
      surface: "rgba(210, 70, 70, 0.12)",
    },
  },
  editor: {
    gutterText: "rgba(128, 128, 128, 0.5)",
    activeLine: "rgba(128, 128, 128, 0.08)",
    selectionMatch: "rgba(80, 140, 255, 0.25)",
    annotationText: "#b9d8ff",
    annotationSurface: "#263c57",
    annotationBorder: "#477db9",
    annotationEnumText: "#ffdc9e",
    annotationEnumSurface: "#4b3920",
    annotationEnumBorder: "#a47a35",
    annotationReferenceText: "#ace8cc",
    annotationReferenceSurface: "#213f34",
    annotationReferenceBorder: "#438a69",
    annotationLink: "rgba(174, 220, 255, 0.8)",
    syntaxNumber: "#d19a66",
    syntaxString: "#98c379",
    syntaxHeading: "#61afef",
  },
  effects: {
    scrollbar: "rgba(128, 128, 128, 0.35)",
    scrollbarHover: "rgba(128, 128, 128, 0.55)",
    tooltipShadow: "rgba(0, 0, 0, 0.38)",
    successGlow: "rgba(99, 226, 183, 0.35)",
    focusRing: "rgba(79, 140, 255, 0.45)",
    activePaneRing: "rgba(79, 140, 255, 0.25)",
    splitHover: "rgba(79, 140, 255, 0.45)",
    successRing: "rgba(99, 226, 183, 0.55)",
    successEdge: "rgba(99, 226, 183, 0.85)",
  },
};

const lightPalette: ThemePalette = {
  window: {
    solidBackground: "#f3f5f8",
    glassBackground: "rgba(243, 245, 248, 0.68)",
  },
  surface: {
    panel: "rgba(255, 255, 255, 0.72)",
    card: "#ffffff",
    elevated: "rgba(255, 255, 255, 0.96)",
    subtle: "rgba(15, 23, 42, 0.035)",
    list: "rgba(15, 23, 42, 0.045)",
    hover: "rgba(56, 111, 232, 0.1)",
    selected: "rgba(56, 111, 232, 0.14)",
    dragPreview: "rgba(56, 111, 232, 0.18)",
    inset: "rgba(15, 23, 42, 0.04)",
    code: "rgba(15, 23, 42, 0.06)",
    warning: "rgba(180, 110, 0, 0.12)",
    success: "rgba(15, 138, 99, 0.12)",
    error: "rgba(194, 59, 75, 0.12)",
  },
  text: {
    primary: "#1f2937",
    secondary: "#334155",
    tertiary: "#475569",
    muted: "#64748b",
    faint: "#7c8797",
    disabled: "#94a3b8",
    onAccent: "#ffffff",
    code: "#334155",
  },
  border: {
    faint: "rgba(15, 23, 42, 0.08)",
    subtle: "rgba(15, 23, 42, 0.12)",
    normal: "rgba(15, 23, 42, 0.16)",
    strong: "rgba(15, 23, 42, 0.24)",
  },
  primary: {
    base: "#386fe8",
    hover: "#2f5fd0",
    pressed: "#264fae",
    soft: "rgba(56, 111, 232, 0.1)",
    selected: "rgba(56, 111, 232, 0.14)",
  },
  status: {
    info: {
      text: "#2f67c7",
      hover: "#2455ac",
      pressed: "#1d468f",
      surface: "rgba(56, 111, 232, 0.1)",
    },
    success: {
      text: "#138a62",
      hover: "#0f6f51",
      pressed: "#0b5940",
      surface: "rgba(15, 138, 99, 0.12)",
    },
    warning: {
      text: "#a15c00",
      hover: "#854b00",
      pressed: "#6d3d00",
      surface: "rgba(180, 110, 0, 0.12)",
    },
    error: {
      text: "#c23b4b",
      hover: "#a92f3f",
      pressed: "#8d2635",
      surface: "rgba(194, 59, 75, 0.12)",
    },
  },
  editor: {
    gutterText: "#94a3b8",
    activeLine: "rgba(15, 23, 42, 0.045)",
    selectionMatch: "rgba(56, 111, 232, 0.2)",
    annotationText: "#1f5798",
    annotationSurface: "#dbeafe",
    annotationBorder: "#8ab4e8",
    annotationEnumText: "#895700",
    annotationEnumSurface: "#fff1c7",
    annotationEnumBorder: "#d7aa43",
    annotationReferenceText: "#176345",
    annotationReferenceSurface: "#d7f3e7",
    annotationReferenceBorder: "#67b793",
    annotationLink: "#245f9e",
    syntaxNumber: "#9a5b16",
    syntaxString: "#257a48",
    syntaxHeading: "#2f6fba",
  },
  effects: {
    scrollbar: "rgba(15, 23, 42, 0.22)",
    scrollbarHover: "rgba(15, 23, 42, 0.35)",
    tooltipShadow: "rgba(15, 23, 42, 0.18)",
    successGlow: "rgba(15, 138, 99, 0.25)",
    focusRing: "rgba(56, 111, 232, 0.4)",
    activePaneRing: "rgba(56, 111, 232, 0.25)",
    splitHover: "rgba(56, 111, 232, 0.4)",
    successRing: "rgba(15, 138, 99, 0.45)",
    successEdge: "rgba(15, 138, 99, 0.7)",
  },
};

export const themeDefinitions: Record<ResolvedThemeId, ThemeDefinition> = {
  dark: {
    id: "dark",
    label: "深色",
    palette: darkPalette,
    naiveTheme: darkTheme,
  },
  light: {
    id: "light",
    label: "浅色",
    palette: lightPalette,
    naiveTheme: lightTheme,
  },
};

export function getTheme(id: ResolvedThemeId): ThemeDefinition {
  return themeDefinitions[id];
}

export function resolveThemeMode(mode: ThemeMode, prefersDark: boolean): ResolvedThemeId {
  if (mode === "system") return prefersDark ? "dark" : "light";
  return mode;
}

export function makeThemeOverrides(theme: ThemeDefinition): GlobalThemeOverrides {
  const { palette: p } = theme;
  const baseColor = theme.id === "dark" ? "#000000" : "#ffffff";

  return {
    common: {
      baseColor,
      primaryColor: p.primary.base,
      primaryColorHover: p.primary.hover,
      primaryColorPressed: p.primary.pressed,
      primaryColorSuppl: p.primary.hover,
      infoColor: p.status.info.text,
      infoColorHover: p.status.info.hover,
      infoColorPressed: p.status.info.pressed,
      infoColorSuppl: p.status.info.hover,
      successColor: p.status.success.text,
      successColorHover: p.status.success.hover,
      successColorPressed: p.status.success.pressed,
      successColorSuppl: p.status.success.hover,
      warningColor: p.status.warning.text,
      warningColorHover: p.status.warning.hover,
      warningColorPressed: p.status.warning.pressed,
      warningColorSuppl: p.status.warning.hover,
      errorColor: p.status.error.text,
      errorColorHover: p.status.error.hover,
      errorColorPressed: p.status.error.pressed,
      errorColorSuppl: p.status.error.hover,
      textColorBase: p.text.primary,
      textColor1: p.text.primary,
      textColor2: p.text.secondary,
      textColor3: p.text.muted,
      textColorDisabled: p.text.disabled,
      placeholderColor: p.text.faint,
      placeholderColorDisabled: p.text.disabled,
      iconColor: p.text.muted,
      iconColorHover: p.text.secondary,
      iconColorPressed: p.text.primary,
      iconColorDisabled: p.text.disabled,
      dividerColor: p.border.subtle,
      borderColor: p.border.normal,
      closeIconColor: p.text.muted,
      closeIconColorHover: p.text.secondary,
      closeIconColorPressed: p.text.primary,
      scrollbarColor: p.effects.scrollbar,
      scrollbarColorHover: p.effects.scrollbarHover,
      progressRailColor: p.surface.inset,
      popoverColor: p.surface.elevated,
      tableColor: p.surface.card,
      cardColor: p.surface.card,
      modalColor: p.surface.card,
      bodyColor: p.window.solidBackground,
      tagColor: p.surface.subtle,
      invertedColor: baseColor,
      inputColor: p.surface.subtle,
      codeColor: p.surface.code,
      tabColor: p.surface.subtle,
      actionColor: p.surface.subtle,
      tableHeaderColor: p.surface.subtle,
      hoverColor: p.surface.hover,
      tableColorHover: p.surface.hover,
      tableColorStriped: p.surface.subtle,
      pressedColor: p.surface.selected,
      inputColorDisabled: p.surface.inset,
      buttonColor2: p.surface.subtle,
      buttonColor2Hover: p.surface.hover,
      buttonColor2Pressed: p.surface.selected,
      boxShadow1: `0 1px 2px -2px ${p.effects.tooltipShadow}, 0 3px 6px 0 ${p.effects.tooltipShadow}`,
      boxShadow2: `0 3px 6px -4px ${p.effects.tooltipShadow}, 0 6px 12px 0 ${p.effects.tooltipShadow}`,
      boxShadow3: `0 6px 16px -9px ${p.effects.tooltipShadow}, 0 9px 28px 0 ${p.effects.tooltipShadow}`,
    },
  };
}

function themeVariables(p: ThemePalette): Record<string, string> {
  return {
    "--pvf-window-background-solid": p.window.solidBackground,
    "--pvf-window-background-glass": p.window.glassBackground,
    "--pvf-surface-panel": p.surface.panel,
    "--pvf-surface-card": p.surface.card,
    "--pvf-surface-elevated": p.surface.elevated,
    "--pvf-surface-subtle": p.surface.subtle,
    "--pvf-surface-list": p.surface.list,
    "--pvf-surface-hover": p.surface.hover,
    "--pvf-surface-selected": p.surface.selected,
    "--pvf-surface-drag-preview": p.surface.dragPreview,
    "--pvf-surface-inset": p.surface.inset,
    "--pvf-surface-code": p.surface.code,
    "--pvf-surface-warning": p.surface.warning,
    "--pvf-surface-success": p.surface.success,
    "--pvf-surface-error": p.surface.error,
    "--pvf-text-primary": p.text.primary,
    "--pvf-text-secondary": p.text.secondary,
    "--pvf-text-tertiary": p.text.tertiary,
    "--pvf-text-muted": p.text.muted,
    "--pvf-text-faint": p.text.faint,
    "--pvf-text-disabled": p.text.disabled,
    "--pvf-text-on-accent": p.text.onAccent,
    "--pvf-text-code": p.text.code,
    "--pvf-border-faint": p.border.faint,
    "--pvf-border-subtle": p.border.subtle,
    "--pvf-border-normal": p.border.normal,
    "--pvf-border-strong": p.border.strong,
    "--pvf-primary": p.primary.base,
    "--pvf-primary-hover": p.primary.hover,
    "--pvf-primary-pressed": p.primary.pressed,
    "--pvf-primary-soft": p.primary.soft,
    "--pvf-primary-selected": p.primary.selected,
    "--pvf-info": p.status.info.text,
    "--pvf-info-hover": p.status.info.hover,
    "--pvf-info-surface": p.status.info.surface,
    "--pvf-success": p.status.success.text,
    "--pvf-success-hover": p.status.success.hover,
    "--pvf-success-surface": p.status.success.surface,
    "--pvf-warning": p.status.warning.text,
    "--pvf-warning-hover": p.status.warning.hover,
    "--pvf-warning-surface": p.status.warning.surface,
    "--pvf-error": p.status.error.text,
    "--pvf-error-hover": p.status.error.hover,
    "--pvf-error-surface": p.status.error.surface,
    "--pvf-editor-gutter-text": p.editor.gutterText,
    "--pvf-editor-active-line": p.editor.activeLine,
    "--pvf-editor-selection-match": p.editor.selectionMatch,
    "--pvf-editor-annotation-text": p.editor.annotationText,
    "--pvf-editor-annotation-surface": p.editor.annotationSurface,
    "--pvf-editor-annotation-border": p.editor.annotationBorder,
    "--pvf-editor-annotation-enum-text": p.editor.annotationEnumText,
    "--pvf-editor-annotation-enum-surface": p.editor.annotationEnumSurface,
    "--pvf-editor-annotation-enum-border": p.editor.annotationEnumBorder,
    "--pvf-editor-annotation-reference-text": p.editor.annotationReferenceText,
    "--pvf-editor-annotation-reference-surface": p.editor.annotationReferenceSurface,
    "--pvf-editor-annotation-reference-border": p.editor.annotationReferenceBorder,
    "--pvf-editor-annotation-link": p.editor.annotationLink,
    "--pvf-editor-syntax-number": p.editor.syntaxNumber,
    "--pvf-editor-syntax-string": p.editor.syntaxString,
    "--pvf-editor-syntax-heading": p.editor.syntaxHeading,
    "--pvf-effect-scrollbar": p.effects.scrollbar,
    "--pvf-effect-scrollbar-hover": p.effects.scrollbarHover,
    "--pvf-effect-tooltip-shadow": p.effects.tooltipShadow,
    "--pvf-effect-success-glow": p.effects.successGlow,
    "--pvf-effect-focus-ring": p.effects.focusRing,
    "--pvf-effect-active-pane-ring": p.effects.activePaneRing,
    "--pvf-effect-split-hover": p.effects.splitHover,
    "--pvf-effect-success-ring": p.effects.successRing,
    "--pvf-effect-success-edge": p.effects.successEdge,
  };
}

export function applyTheme(theme: ThemeDefinition): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  for (const [name, value] of Object.entries(themeVariables(theme.palette))) {
    root.style.setProperty(name, value);
  }
  root.dataset.theme = theme.id;
  root.style.colorScheme = theme.id;
}
