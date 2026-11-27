<script lang="ts">
  import { autocompletion, type CompletionContext, type CompletionResult } from '@codemirror/autocomplete';
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
  import {
    HighlightStyle,
    LanguageDescription,
    syntaxHighlighting,
    syntaxTree
  } from '@codemirror/language';
  import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
  import { Compartment, EditorState } from '@codemirror/state';
  import { EditorView, keymap, placeholder } from '@codemirror/view';
  import { tags } from '@lezer/highlight';
  import { onMount } from 'svelte';

  import {
    codeBlockEscape,
    isClosingFenceAtEnd,
    liveMarkdown,
    type WikilinkClick
  } from '#lib/editor/live-markdown';

  interface Props {
    value: string;
    onchange?: (value: string) => void;
    onsave?: () => void;
    onwikilink?: (link: WikilinkClick) => void;
    onlink?: (href: string) => void;
    readonly?: boolean;
    label?: string;
    getLinkTargets?: () => string[];
  }

  let {
    value,
    onchange,
    onsave,
    onwikilink,
    onlink,
    readonly = false,
    label = 'Markdown body',
    getLinkTargets
  }: Props = $props();

  let host = $state<HTMLDivElement>();
  let view = $state.raw<EditorView>();
  let applying = false;

  const readOnly = new Compartment();

  const codeLanguages = [
    LanguageDescription.of({
      name: 'Python',
      alias: ['python', 'py'],
      load: () => import('@codemirror/lang-python').then((module) => module.python())
    }),
    LanguageDescription.of({
      name: 'JavaScript',
      alias: ['javascript', 'js', 'jsx'],
      load: () =>
        import('@codemirror/lang-javascript').then((module) => module.javascript({ jsx: true }))
    }),
    LanguageDescription.of({
      name: 'TypeScript',
      alias: ['typescript', 'ts', 'tsx'],
      load: () =>
        import('@codemirror/lang-javascript').then((module) =>
          module.javascript({ jsx: true, typescript: true })
        )
    }),
    LanguageDescription.of({
      name: 'JSON',
      alias: ['json', 'jsonc'],
      load: () => import('@codemirror/lang-json').then((module) => module.json())
    }),
    LanguageDescription.of({
      name: 'HTML',
      alias: ['html'],
      load: () => import('@codemirror/lang-html').then((module) => module.html())
    }),
    LanguageDescription.of({
      name: 'CSS',
      alias: ['css'],
      load: () => import('@codemirror/lang-css').then((module) => module.css())
    }),
    LanguageDescription.of({
      name: 'SQL',
      alias: ['sql'],
      load: () => import('@codemirror/lang-sql').then((module) => module.sql())
    }),
    LanguageDescription.of({
      name: 'Go',
      alias: ['go', 'golang'],
      load: () => import('@codemirror/lang-go').then((module) => module.go())
    }),
    LanguageDescription.of({
      name: 'Rust',
      alias: ['rust', 'rs'],
      load: () => import('@codemirror/lang-rust').then((module) => module.rust())
    }),
    LanguageDescription.of({
      name: 'YAML',
      alias: ['yaml', 'yml'],
      load: () => import('@codemirror/lang-yaml').then((module) => module.yaml())
    })
  ];

  const highlightStyle = HighlightStyle.define([
    { tag: [tags.keyword, tags.modifier], color: 'oklch(0.78 0.12 310)' },
    { tag: [tags.string, tags.special(tags.string)], color: 'oklch(0.8 0.1 150)' },
    { tag: [tags.number, tags.bool, tags.null], color: 'oklch(0.82 0.1 80)' },
    {
      tag: [tags.comment, tags.lineComment, tags.blockComment],
      color: 'oklch(0.6 0.02 285)',
      fontStyle: 'italic'
    },
    {
      tag: [tags.function(tags.variableName), tags.function(tags.propertyName)],
      color: 'oklch(0.8 0.1 235)'
    },
    { tag: [tags.typeName, tags.className, tags.namespace], color: 'oklch(0.8 0.09 190)' },
    { tag: [tags.propertyName, tags.attributeName, tags.tagName], color: 'oklch(0.8 0.08 100)' },
    { tag: [tags.operator, tags.punctuation], color: 'oklch(0.7 0.02 285)' },
    { tag: [tags.regexp, tags.escape], color: 'oklch(0.82 0.1 80)' },
    { tag: tags.meta, color: 'oklch(0.72 0.05 300)' },
    { tag: tags.invalid, color: 'oklch(0.72 0.15 25)' }
  ]);

  function insideCode(state: EditorState, pos: number): boolean {
    let node = syntaxTree(state).resolveInner(pos, -1);
    while (node) {
      if (node.name === 'FencedCode' || node.name === 'CodeBlock' || node.name === 'InlineCode') {
        return true;
      }
      node = node.parent!;
    }
    return false;
  }

  function clamp(value: number, min: number, max: number): number {
    return Math.min(Math.max(value, min), Math.max(min, max));
  }

  function onSurfaceMouseDown(event: MouseEvent) {
    const editor = view;
    if (!editor || event.button !== 0) return;
    const target = event.target;
    if (!(target instanceof Element)) return;
    if (
      target.closest(
        '.cm-line, .cm-widgetBuffer, .cm-live-table-wrap, .cm-tooltip, .cm-tooltip-autocomplete, .cm-gutter, .cm-gutterElement'
      )
    ) {
      return;
    }

    if (isClosingFenceAtEnd(editor.state)) {
      const last = editor.state.doc.line(editor.state.doc.lines);
      const bottom = editor.coordsAtPos(last.to)?.bottom;
      if (bottom != null && event.clientY > bottom + 2) {
        event.preventDefault();
        const pos = editor.state.doc.length;
        editor.dispatch({
          changes: { from: pos, insert: '\n' },
          selection: { anchor: pos + 1 },
          scrollIntoView: true
        });
        editor.focus();
        return;
      }
    }

    const rect = editor.contentDOM.getBoundingClientRect();
    const x = clamp(event.clientX, rect.left + 1, rect.right - 1);
    const y = clamp(event.clientY, rect.top + 1, rect.bottom - 1);
    const pos = editor.posAtCoords({ x, y }) ?? editor.state.doc.length;
    event.preventDefault();
    editor.dispatch({ selection: { anchor: pos }, scrollIntoView: true });
    editor.focus();
  }

  function wikilinkCompletions(context: CompletionContext): CompletionResult | null {
    const before = context.state.sliceDoc(Math.max(0, context.pos - 400), context.pos);
    const match = /(!?)\[\[([^\]\n]*)$/.exec(before);
    if (!match) return null;
    if (insideCode(context.state, context.pos)) return null;

    const query = match[2];
    const from = context.pos - query.length;
    const targets = [...new Set(getLinkTargets?.() ?? [])].sort((a, b) => a.localeCompare(b));
    if (!targets.length) return null;

    return {
      from,
      options: targets.map((path) => {
        const label = path.replace(/\.md$/i, '');
        const slash = label.lastIndexOf('/');
        return {
          label,
          detail: slash >= 0 ? label.slice(0, slash) : undefined,
          apply: (targetView, _completion, applyFrom, applyTo) => {
            const after = targetView.state.doc.sliceString(
              applyTo,
              Math.min(applyTo + 2, targetView.state.doc.length)
            );
            const suffix = after.startsWith(']]') ? '' : ']]';
            targetView.dispatch({
              changes: { from: applyFrom, to: applyTo, insert: label + suffix },
              selection: { anchor: applyFrom + label.length }
            });
          }
        };
      }),
      validFor: /^[^\]\n]*$/
    };
  }

  onMount(() => {
    const surface = host;
    surface?.addEventListener('mousedown', onSurfaceMouseDown);
    const instance = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          history(),
          keymap.of([
            {
              key: 'ArrowDown',
              run: (targetView) => {
                const escape = codeBlockEscape(targetView.state);
                if (!escape) return false;
                targetView.dispatch({
                  changes: { from: escape.from, insert: escape.insert },
                  selection: { anchor: escape.anchor },
                  scrollIntoView: true
                });
                return true;
              }
            },
            ...defaultKeymap,
            ...historyKeymap,
            indentWithTab,
            {
              key: 'Mod-s',
              preventDefault: true,
              run: () => {
                onsave?.();
                return true;
              }
            }
          ]),
          markdown({ base: markdownLanguage, codeLanguages, completeHTMLTags: false }),
          syntaxHighlighting(highlightStyle),
          autocompletion({ override: [wikilinkCompletions], activateOnTyping: true }),
          EditorView.lineWrapping,
          placeholder('Start writing…'),
          EditorView.contentAttributes.of({ 'aria-label': label, spellcheck: 'true' }),
          readOnly.of(EditorState.readOnly.of(readonly)),
          liveMarkdown(),
          editorTheme,
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !applying) onchange?.(update.state.doc.toString());
          }),
          EditorView.domEventHandlers({
            click(event, editor) {
              if (!(event.metaKey || event.ctrlKey)) return false;
              // Cmd/Ctrl-click follows links; a plain click always edits.
              const pos = editor.posAtCoords({ x: event.clientX, y: event.clientY });
              if (pos == null) return false;
              const line = editor.state.doc.lineAt(pos);
              const offset = pos - line.from;
              const wikilink = findWikilink(line.text, offset);
              if (wikilink) {
                event.preventDefault();
                onwikilink?.(wikilink);
                return true;
              }
              const href = findMarkdownLink(line.text, offset);
              if (href) {
                event.preventDefault();
                onlink?.(href);
                return true;
              }
              return false;
            }
          })
        ]
      }),
      parent: host
    });
    view = instance;

    return () => {
      surface?.removeEventListener('mousedown', onSurfaceMouseDown);
      instance.destroy();
      if (view === instance) view = undefined;
    };
  });

  $effect(() => {
    const instance = view;
    if (!instance) return;
    const next = value;
    const current = instance.state.doc.toString();
    if (current === next) return;
    applying = true;
    instance.dispatch({ changes: { from: 0, to: current.length, insert: next } });
    applying = false;
  });

  $effect(() => {
    const instance = view;
    if (!instance) return;
    instance.dispatch({
      effects: readOnly.reconfigure(EditorState.readOnly.of(readonly))
    });
  });

  function findWikilink(text: string, offset: number): WikilinkClick | null {
    const pattern = /(!?)\[\[([^\]\n]+)\]\]/g;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(text))) {
      const start = match.index;
      const end = start + match[0].length;
      if (offset < start || offset > end) continue;
      const inner = match[2];
      const pipe = inner.indexOf('|');
      const hash = inner.indexOf('#');
      const targetEnd = minimum(inner.length, pipe, hash);
      const target = inner.slice(0, targetEnd).trim();
      if (!target) continue;
      return {
        target,
        kind: match[1] ? 'embed' : 'link',
        alias: pipe >= 0 ? inner.slice(pipe + 1).trim() : undefined,
        fragment: hash >= 0 ? inner.slice(hash + 1).trim() : undefined
      };
    }
    return null;
  }

  function findMarkdownLink(text: string, offset: number): string | null {
    const pattern = /(?<!!)\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(text))) {
      const start = match.index;
      const end = start + match[0].length;
      if (offset >= start && offset <= end) return match[2];
    }
    return null;
  }

  function minimum(fallback: number, ...values: number[]): number {
    let result = fallback;
    for (const value of values) {
      if (value >= 0 && value < result) result = value;
    }
    return result;
  }

  const editorTheme = EditorView.theme(
    {
      '&': {
        backgroundColor: 'transparent',
        color: 'inherit',
        fontSize: '1rem'
      },
      '.cm-scroller': {
        fontFamily: 'var(--font-sans)',
        overflow: 'visible'
      },
      '.cm-content': {
        padding: '0',
        caretColor: 'oklch(0.92 0.004 285)'
      },
      '.cm-line': { padding: '0' },
      '&.cm-focused': { outline: 'none' },
      '.cm-cursor, .cm-dropCursor': {
        borderLeftColor: 'oklch(0.92 0.004 285)'
      },
      '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
        backgroundColor: 'oklch(0.45 0.08 245 / 0.35)'
      },
      '.cm-placeholder': { color: 'oklch(0.5 0.01 285)' },
      '.cm-tooltip-autocomplete': {
        backgroundColor: 'oklch(0.2 0.007 285)',
        border: '1px solid var(--color-border-subtle)',
        borderRadius: '0.5rem',
        boxShadow: '0 12px 30px rgb(0 0 0 / 0.35)',
        overflow: 'hidden'
      },
      '.cm-tooltip-autocomplete > ul': {
        fontFamily: 'var(--font-sans)',
        maxHeight: '18rem'
      },
      '.cm-tooltip-autocomplete > ul > li': { padding: '0.35rem 0.6rem' },
      '.cm-tooltip-autocomplete > ul > li[aria-selected]': {
        backgroundColor: 'oklch(0.3 0.03 245)',
        color: 'oklch(0.97 0.004 285)'
      },
      '.cm-completionDetail': {
        color: 'oklch(0.62 0.01 285)',
        fontStyle: 'normal',
        marginLeft: '0.5rem'
      },
      '.cm-completionMatchedText': {
        textDecoration: 'none',
        color: 'oklch(0.82 0.1 235)'
      }
    },
    { dark: true }
  );
</script>

<div class="markdown-editor" bind:this={host}></div>
