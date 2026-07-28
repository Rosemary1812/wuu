# Desktop i18n maintenance

This directory is maintained primarily through coding agents. Keep the rules
executable: do not rely on a reviewer noticing a missing locale or placeholder.

## Change a user-facing string

1. Reuse an existing semantic key when the meaning is the same.
2. Otherwise add the key to `resources/zh-CN.ts` and `resources/en-US.ts` in the
   same position. Use `domain.camelCaseName`; do not use English copy as a key.
3. Keep placeholders identical across locales, including case.
4. Run the focused i18n tests and desktop typecheck.

Renderer components use `useI18n().t`. Non-React renderer helpers use
`translateCurrent`. State that must change language after it is created uses
`localizedText` and resolves it only at the display boundary. Native Electron
menus and windows use the smaller catalog in `src/main/i18n.ts`.

Use the i18n number/date helpers for display. Do not call `toLocaleString` with
the system default because the user may select a different Wuu language.

## Add a locale

1. Add it once to `APP_LOCALES` in `packages/protocol/src/index.ts`.
2. Add its renderer resource and register it in `i18n/index.tsx` and
   `catalogContract.test.ts`.
3. Add its language-picker label in `LanguagePreferenceSection.tsx`.
4. Add its native resource in `src/main/i18n.ts`.
5. Update `resolveAppLocale` in the protocol if the new locale should match an
   OS locale family.

The registry-derived types intentionally make incomplete steps fail typecheck.
`catalogContract.test.ts` then checks runtime invariants that TypeScript cannot:
key order, blank values, and placeholder parity.
