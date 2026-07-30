# wuu documentation

The documentation in this directory is organized as publishable content, not as
a loose collection of repository notes.

- [简体中文入口](zh-cn/index.md)
- [English entry](en/index.md)
- [Site manifest](site.json)

## Content structure

```text
docs/
├── site.json                 # site identity, locales, and navigation
├── zh-cn/                    # Simplified Chinese content
│   ├── getting-started/      # first successful task
│   ├── desktop/              # daily desktop workflows
│   ├── customize/            # skills, memory, and user-owned behavior
│   ├── help/                 # symptom-led troubleshooting
│   └── reference/            # permissions, config, and security boundaries
├── en/                       # English content
│   ├── getting-started/
│   ├── automation/
│   ├── integrations/
│   ├── reference/
│   └── project/
└── plans/                    # ignored working notes; never published
```

The current tree keeps all maintained legacy documents, but groups them by
reader intent. A page may link to another language when no translation exists;
the link label should make that fallback clear.

## Authoring rules

- Write standard Markdown so the same source works on GitHub and in a future
  wuu publishing flow.
- Use relative links between pages and keep images in a nearby `assets/`
  directory.
- Describe behavior that exists today. Keep proposals and temporary task notes
  out of the published navigation.
- Treat current code, tests, CLI help, and a verified product path as the source
  of truth. Existing documentation is a lead, not proof that a feature still
  behaves the same way.
- Keep feature-flagged, development-only, and experimental surfaces out of the
  stable user path unless the page labels their availability explicitly.
- Add a page to `site.json` when it should appear in the published site.
- Prefer one task-oriented page over a large feature inventory.

The manifest is intentionally small and renderer-neutral. It defines the
content contract without binding the repository to Astro, MDX, or a hosted
documentation service.
