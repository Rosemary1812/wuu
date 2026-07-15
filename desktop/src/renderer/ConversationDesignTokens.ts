export type ConversationDesignToken = {
  readonly key: string;
  readonly cssVar: string;
  readonly label: string;
  readonly min: number;
  readonly max: number;
  readonly step: number;
  readonly unit: "" | "px";
  readonly defaultValue: number;
};

/*
 * Single registry for the conversation debug mixer.
 *
 * Active tokens are the only CSS variables the panel may write. Legacy
 * names stay below only so stale localStorage and inline styles can be
 * removed during development.
 */
export const CONVERSATION_READING_LINE_HEIGHT_CSS_VAR =
  "--conversation-reading-line-height";

export const CONVERSATION_DESIGN_TOKENS = [
  {
    key: "flow-width",
    cssVar: "--session-outer-width",
    label: "消息流宽度",
    min: 640,
    max: 1280,
    step: 16,
    unit: "px",
    defaultValue: 776,
  },
  {
    key: "message-max-width",
    cssVar: "--conversation-message-max-width",
    label: "消息最大宽度",
    min: 480,
    max: 1080,
    step: 16,
    unit: "px",
    defaultValue: 680,
  },
  {
    key: "composer-width",
    cssVar: "--session-composer-width",
    label: "输入框宽度",
    min: 480,
    max: 1200,
    step: 20,
    unit: "px",
    defaultValue: 680,
  },
  {
    key: "composer-radius",
    cssVar: "--session-composer-radius",
    label: "输入框圆角",
    min: 0,
    max: 32,
    step: 1,
    unit: "px",
    defaultValue: 18,
  },
  {
    key: "msg-font-size",
    cssVar: "--conversation-message-font-size",
    label: "正文字号",
    min: 13,
    max: 20,
    step: 0.5,
    unit: "px",
    defaultValue: 14,
  },
  {
    key: "prose-line-height",
    cssVar: CONVERSATION_READING_LINE_HEIGHT_CSS_VAR,
    label: "正文行高",
    min: 1.5,
    max: 2.3,
    step: 0.02,
    unit: "",
    defaultValue: 1.75,
  },
  {
    key: "prose-block-gap",
    cssVar: "--conversation-prose-block-gap",
    label: "段落间距",
    min: 4,
    max: 48,
    step: 1,
    unit: "px",
    defaultValue: 18,
  },
  {
    key: "meta-line-height",
    cssVar: "--conversation-meta-line-height",
    label: "Meta 行高",
    min: 1.2,
    max: 1.8,
    step: 0.05,
    unit: "",
    defaultValue: 1.6,
  },
  {
    key: "control-line-height",
    cssVar: "--conversation-control-line-height",
    label: "控件行高",
    min: 1.2,
    max: 2.2,
    step: 0.05,
    unit: "",
    defaultValue: 1.7,
  },
  {
    key: "process-gap",
    cssVar: "--conversation-process-detail-gap",
    label: "过程详情间距",
    min: 2,
    max: 32,
    step: 1,
    unit: "px",
    defaultValue: 16,
  },
  {
    key: "message-element-gap",
    cssVar: "--conversation-message-element-gap",
    label: "消息块间距",
    min: 4,
    max: 32,
    step: 1,
    unit: "px",
    defaultValue: 18,
  },
  {
    key: "turn-gap",
    cssVar: "--conversation-turn-item-gap",
    label: "Turn 内相邻项间距",
    min: 0,
    max: 24,
    step: 1,
    unit: "px",
    defaultValue: 8,
  },
  {
    key: "turn-boundary-gap",
    cssVar: "--conversation-turn-boundary-gap",
    label: "Turn 之间间距",
    min: 8,
    max: 48,
    step: 4,
    unit: "px",
    defaultValue: 32,
  },
  {
    key: "flow-top-gap",
    cssVar: "--conversation-flow-top-gap",
    label: "首条消息顶部留白",
    min: 24,
    max: 96,
    step: 4,
    unit: "px",
    defaultValue: 36,
  },
  {
    key: "flow-padding",
    cssVar: "--session-outer-padding-inline",
    label: "流两侧留白",
    min: 24,
    max: 96,
    step: 4,
    unit: "px",
    defaultValue: 48,
  },
] as const satisfies readonly ConversationDesignToken[];

export type ConversationDesignTokenKey =
  (typeof CONVERSATION_DESIGN_TOKENS)[number]["key"];

export const CONVERSATION_DESIGN_TOKEN_STORAGE_KEY =
  "wuu:design-tokens:v4";

export const LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS = [
  "wuu:design-tokens:v2",
  "wuu:design-tokens:v3",
] as const;

export const LEGACY_CONVERSATION_DESIGN_TOKEN_CSS_VARS = [
  "--conversation-readable-width",
  "--conversation-flow-padding-inline",
  "--conversation-dialog-width",
  "--conversation-dialog-radius",
  "--conversation-prose-line-height",
  "--conversation-process-gap",
  "--conversation-turn-gap",
] as const;

export function conversationDesignTokenByKey(
  key: string,
): ConversationDesignToken | undefined {
  return CONVERSATION_DESIGN_TOKENS.find((token) => token.key === key);
}

export function clampConversationDesignTokenValue(
  token: ConversationDesignToken,
  value: number,
): number {
  return Math.min(token.max, Math.max(token.min, value));
}

export function conversationDesignTokenStyleValue(
  token: ConversationDesignToken,
  value: number,
): string {
  return `${value}${token.unit}`;
}
