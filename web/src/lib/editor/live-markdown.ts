// A Typora-like layer for CodeMirror: markdown stays the source of truth, but
// inactive lines are styled as rendered text. Formatting marks remain visible
// on the line holding the cursor so editing never hides the syntax under it.
import { syntaxTree } from '@codemirror/language';
import { StateField, type EditorState, type Extension } from '@codemirror/state';
import {
  Decoration,
  EditorView,
  ViewPlugin,
  WidgetType,
  type DecorationSet,
  type ViewUpdate
} from '@codemirror/view';

import { fileHref } from '../api';

export interface LiveMarkdownOptions {
  resolveImage?: (src: string) => string;
}

export interface WikilinkClick {
  target: string;
  kind: 'link' | 'embed';
  alias?: string;
  fragment?: string;
}

interface PendingDecoration {
  from: number;
  to: number;
  decoration: Decoration;
}

interface CodeRange {
  from: number;
  to: number;
}

export interface CodeBlockEscape {
  from: number;
  insert: string;
  anchor: number;
}

export interface ParsedTable {
  source: string;
  header: string[];
  rows: string[][];
  align: ('left' | 'center' | 'right')[];
  lineNumbers: number[];
}

const WIKILINK = /(!?)\[\[([^\]\n]+)\]\]/g;
const MARKDOWN_IMAGE = /^!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)$/;
const IMAGE_PATH = /\.(png|jpe?g|gif|webp|svg|avif|pdf)$/i;
const TAG = /(^|[\s([{>])#([\p{L}\p{N}_][\p{L}\p{N}_/-]*)/gu;
const TABLE_INLINE =
  /(`[^`\n]+`|\*\*[^*\n]+\*\*|__[^_\n]+__|~~[^~\n]+~~|\*[^*\n]+\*|_[^_\n]+_|\[\[[^\]\n]+\]\]|\[[^\]\n]+\]\([^)\n]+\))/g;

const HEADING = /^ATXHeading([1-6])$/;

const hidden = Decoration.replace({});
const strongMark = Decoration.mark({ class: 'cm-strong' });
const emphasisMark = Decoration.mark({ class: 'cm-em' });
const inlineCodeMark = Decoration.mark({ class: 'cm-inline-code' });
const strikeMark = Decoration.mark({ class: 'cm-strike' });
const linkMark = Decoration.mark({ class: 'cm-link' });
const listMark = Decoration.mark({ class: 'cm-list-mark' });
const tagMark = Decoration.mark({ class: 'cm-md-tag' });
const codeLine = Decoration.line({ class: 'cm-code-block' });
const quoteLine = Decoration.line({ class: 'cm-blockquote' });
const ruleLine = Decoration.line({ class: 'cm-hr' });

export const tableWidgets = StateField.define<DecorationSet>({
  create(state) {
    return buildTableWidgets(state);
  },
  update(decorations, transaction) {
    if (!transaction.docChanged && !transaction.selection) return decorations;
    return buildTableWidgets(transaction.state);
  },
  provide: (field) => EditorView.decorations.from(field)
});

export function liveMarkdown(options: LiveMarkdownOptions = {}): Extension {
  return [
    tableWidgets,
    ViewPlugin.fromClass(
      class {
        decorations: DecorationSet;

        constructor(view: EditorView) {
          this.decorations = build(view, options);
        }

        update(update: ViewUpdate) {
          if (update.docChanged || update.selectionSet || update.viewportChanged) {
            this.decorations = build(update.view, options);
          }
        }
      },
      {
        decorations: (plugin) => plugin.decorations
      }
    )
  ];
}

function build(view: EditorView, options: LiveMarkdownOptions): DecorationSet {
  return buildDecorations(view.state, view.visibleRanges, options);
}

export function buildDecorations(
  state: EditorState,
  visibleRanges: readonly { from: number; to: number }[],
  options: LiveMarkdownOptions = {}
): DecorationSet {
  const active = activeLines(state);
  const pending: PendingDecoration[] = [];
  const codeRanges: CodeRange[] = [];
  const excludedRanges: CodeRange[] = [];
  const replacedRanges: CodeRange[] = [];

  const add = (from: number, to: number, decoration: Decoration) => {
    if (from >= to && decoration !== ruleLine) return;
    if (decoration === ruleLine && from !== to) return;
    pending.push({ from, to, decoration });
  };
  const addLine = (pos: number, decoration: Decoration) => {
    const line = state.doc.lineAt(pos);
    pending.push({ from: line.from, to: line.from, decoration });
  };
  const hide = (from: number, to: number) => {
    if (to > from) add(from, to, hidden);
  };
  const lineActive = (pos: number) => active.has(state.doc.lineAt(pos).number);
  const exclude = (from: number, to: number) => {
    if (to > from) excludedRanges.push({ from, to });
  };
  syntaxTree(state).iterate({
    enter(node) {
      const name = node.name;
      const isActive = lineActive(node.from);
      const heading = HEADING.exec(name);

      if (heading) {
        addLine(node.from, Decoration.line({ class: `cm-heading cm-heading-${heading[1]}` }));
        return;
      }

      if (name === 'Table') {
        // Table blocks are provided by tableWidgets, a StateField. Block
        // decorations are not allowed from a ViewPlugin.
        exclude(node.from, node.to);
        return false;
      }

      if (name === 'FencedCode' || name === 'CodeBlock') {
        codeRanges.push({ from: node.from, to: node.to });
        exclude(node.from, node.to);
        addLineRange(state, node.from, node.to, codeLine, pending);
        return;
      }

      if (name === 'InlineCode') {
        codeRanges.push({ from: node.from, to: node.to });
        exclude(node.from, node.to);
        add(node.from, node.to, inlineCodeMark);
        return;
      }

      if (name === 'Blockquote') {
        addLineRange(state, node.from, node.to, quoteLine, pending);
        return;
      }

      if (name === 'StrongEmphasis') {
        add(node.from, node.to, strongMark);
        return;
      }

      if (name === 'Emphasis') {
        add(node.from, node.to, emphasisMark);
        return;
      }

      if (name === 'Strikethrough') {
        add(node.from, node.to, strikeMark);
        return;
      }

      if (name === 'ListMark') {
        add(node.from, node.to, listMark);
        return;
      }

      if (name === 'Link') {
        exclude(node.from, node.to);
        if (isWikilink(state, node.from, 'Link')) return false;
        add(node.from, node.to, linkMark);
        return;
      }

      if (name === 'Image') {
        exclude(node.from, node.to);
        if (isWikilink(state, node.from, 'Image')) return false;
        if (!isActive) {
          const raw = state.doc.sliceString(node.from, node.to);
          const parsed = MARKDOWN_IMAGE.exec(raw);
          if (parsed) {
            const src = resolveImage(parsed[2], options);
            add(node.from, node.to, Decoration.replace({ widget: new ImageWidget(src, parsed[1]) }));
            replacedRanges.push({ from: node.from, to: node.to });
            return false;
          }
        }
        return;
      }

      if (name === 'HorizontalRule') {
        addLine(node.from, ruleLine);
        if (!isActive) hide(node.from, node.to);
        return;
      }

      if (
        name === 'HeaderMark' ||
        name === 'EmphasisMark' ||
        name === 'StrikethroughMark' ||
        name === 'CodeMark' ||
        name === 'CodeInfo' ||
        name === 'QuoteMark' ||
        name === 'LinkMark' ||
        name === 'LinkTitle' ||
        name === 'URL'
      ) {
        if (!isActive) hide(node.from, node.to);
      }
    }
  });

  addWikilinks(
    state,
    visibleRanges,
    active,
    codeRanges,
    excludedRanges,
    replacedRanges,
    pending,
    options
  );
  addTags(state, visibleRanges, excludedRanges, pending);

  return Decoration.set(
    pending.map(({ from, to, decoration }) => decoration.range(from, to)),
    true
  );
}

function activeLines(state: EditorState): Set<number> {
  const lines = new Set<number>();
  for (const range of state.selection.ranges) {
    const start = state.doc.lineAt(range.from).number;
    const end = state.doc.lineAt(range.to).number;
    for (let line = start; line <= end; line++) lines.add(line);
  }
  return lines;
}

function buildTableWidgets(state: EditorState): DecorationSet {
  const active = activeLines(state);
  const pending: PendingDecoration[] = [];

  syntaxTree(state).iterate({
    enter(node) {
      if (node.name !== 'Table') return;
      if (rangeIsActive(state, active, node.from, node.to)) return false;
      const table = parseMarkdownTable(
        state.doc.sliceString(node.from, node.to),
        state.doc.lineAt(node.from).number
      );
      if (table) {
        pending.push({
          from: node.from,
          to: node.to,
          decoration: Decoration.replace({
            widget: new TableWidget(table, node.from),
            block: true
          })
        });
      }
      return false;
    }
  });

  return Decoration.set(
    pending.map(({ from, to, decoration }) => decoration.range(from, to)),
    true
  );
}

function rangeIsActive(
  state: EditorState,
  active: Set<number>,
  from: number,
  to: number
): boolean {
  const start = state.doc.lineAt(from).number;
  const end = state.doc.lineAt(Math.min(to, state.doc.length)).number;
  for (let line = start; line <= end; line++) {
    if (active.has(line)) return true;
  }
  return false;
}

function addLineRange(
  state: EditorState,
  from: number,
  to: number,
  decoration: Decoration,
  pending: PendingDecoration[]
) {
  let pos = from;
  while (pos <= to) {
    const line = state.doc.lineAt(pos);
    pending.push({ from: line.from, to: line.from, decoration });
    if (line.to >= to) break;
    pos = line.to + 1;
  }
}

function isWikilink(state: EditorState, from: number, name: 'Link' | 'Image'): boolean {
  const before = from > 0 ? state.doc.sliceString(from - 1, from + 3) : state.doc.sliceString(from, from + 3);
  const marker = state.doc.sliceString(from, from + 3);
  return name === 'Image' ? marker === '![[' || before === '![[' : marker.startsWith('[[') || before === '[[';
}

function addWikilinks(
  state: EditorState,
  visibleRanges: readonly { from: number; to: number }[],
  active: Set<number>,
  codeRanges: CodeRange[],
  excludedRanges: CodeRange[],
  replacedRanges: CodeRange[],
  pending: PendingDecoration[],
  options: LiveMarkdownOptions
) {
  for (const range of visibleRanges) {
    const start = state.doc.lineAt(range.from).number;
    const end = state.doc.lineAt(range.to).number;
    for (let lineNumber = start; lineNumber <= end; lineNumber++) {
      const line = state.doc.line(lineNumber);
      const isActive = active.has(lineNumber);
      for (const match of line.text.matchAll(WIKILINK)) {
        const offset = match.index ?? 0;
        const from = line.from + offset;
        const matchEnd = from + match[0].length;
        if (insideCode(from, codeRanges) || overlaps(from, matchEnd, replacedRanges)) continue;

        const bang = match[1] === '!';
        const inner = match[2];
        const innerStart = from + (bang ? 3 : 2);
        const innerEnd = from + match[0].length - 2;
        const pipe = inner.indexOf('|');
        const hash = inner.indexOf('#');
        const targetEnd = minimum(inner.length, pipe, hash);
        const target = inner.slice(0, targetEnd).trim();
        const alias = pipe >= 0 ? inner.slice(pipe + 1).trim() : '';
        excludedRanges.push({ from, to: matchEnd });
        if (!target) continue;

        if (!isActive && bang && IMAGE_PATH.test(target)) {
          addReplacement(
            pending,
            from,
            from + match[0].length,
            new ImageWidget(resolveImage(target, options), alias || target)
          );
          replacedRanges.push({ from, to: matchEnd });
          continue;
        }

        if (!isActive) {
          addHidden(pending, from, innerStart);
          addHidden(pending, innerEnd, matchEnd);
        }

        let displayStart = innerStart;
        let displayEnd = innerEnd;
        if (pipe >= 0 && alias) {
          displayStart = innerStart + pipe + 1;
          if (!isActive) addHidden(pending, innerStart, displayStart);
        } else if (hash >= 0) {
          displayEnd = innerStart + hash;
          if (!isActive) addHidden(pending, displayEnd, innerEnd);
        }

        if (displayEnd > displayStart) {
          pending.push({
            from: displayStart,
            to: displayEnd,
            decoration: Decoration.mark({
              class: bang ? 'cm-wikilink cm-wikilink-embed' : 'cm-wikilink'
            })
          });
        }
      }
    }
  }
}

function addTags(
  state: EditorState,
  visibleRanges: readonly { from: number; to: number }[],
  excludedRanges: CodeRange[],
  pending: PendingDecoration[]
) {
  for (const range of visibleRanges) {
    const start = state.doc.lineAt(range.from).number;
    const end = state.doc.lineAt(range.to).number;
    for (let lineNumber = start; lineNumber <= end; lineNumber++) {
      const line = state.doc.line(lineNumber);
      for (const match of line.text.matchAll(TAG)) {
        const prefix = match[1] ?? '';
        const label = match[2];
        if (!label) continue;
        const from = line.from + (match.index ?? 0) + prefix.length;
        const to = from + 1 + label.length;
        if (overlaps(from, to, excludedRanges)) continue;
        pending.push({ from, to, decoration: tagMark });
      }
    }
  }
}

function overlaps(from: number, to: number, ranges: CodeRange[]): boolean {
  return ranges.some((range) => from < range.to && to > range.from);
}

function minimum(fallback: number, ...values: number[]): number {
  let result = fallback;
  for (const value of values) {
    if (value >= 0 && value < result) result = value;
  }
  return result;
}

function insideCode(pos: number, ranges: CodeRange[]): boolean {
  return ranges.some((range) => pos >= range.from && pos < range.to);
}

function addHidden(pending: PendingDecoration[], from: number, to: number) {
  if (to > from) pending.push({ from, to, decoration: hidden });
}

function addReplacement(pending: PendingDecoration[], from: number, to: number, widget: WidgetType) {
  pending.push({ from, to, decoration: Decoration.replace({ widget }) });
}

export function isClosingFenceAtEnd(state: EditorState): boolean {
  const doc = state.doc;
  const last = doc.line(doc.lines);
  const closing = /^\s*(`{3,}|~{3,})\s*$/.exec(last.text);
  if (!closing) return false;
  const closingChar = closing[1][0];
  const closingLength = closing[1].length;

  let open: { char: string; length: number } | null = null;
  for (let lineNumber = 1; lineNumber < doc.lines; lineNumber++) {
    const match = /^\s*(`{3,}|~{3,})(.*)$/.exec(doc.line(lineNumber).text);
    if (!match) continue;
    const fence = match[1];
    const char = fence[0];
    const length = fence.length;
    if (!open) {
      open = { char, length };
      continue;
    }
    if (char === open.char && length >= open.length && !match[2].trim()) open = null;
  }
  return open !== null && open.char === closingChar && closingLength >= open.length;
}

/**
 * The edit ArrowDown should make when it is pressed on the final line of a
 * fenced code block. A closed fence gets the blank line it is missing. An
 * unclosed fence at EOF is closed first, otherwise a blank line would still be
 * inside the code block and the cursor could never leave it.
 */
export function codeBlockEscape(state: EditorState): CodeBlockEscape | null {
  const selection = state.selection.main;
  if (!selection.empty) return null;

  const doc = state.doc;
  if (doc.lineAt(selection.head).number !== doc.lines) return null;

  if (isClosingFenceAtEnd(state)) {
    return { from: doc.length, insert: '\n', anchor: doc.length + 1 };
  }

  const open = openFenceAtEnd(state);
  if (!open) return null;
  const prefix = doc.length > 0 && doc.sliceString(doc.length - 1) === '\n' ? '' : '\n';
  const insert = `${prefix}${open.indent}${open.char.repeat(open.length)}\n`;
  return { from: doc.length, insert, anchor: doc.length + insert.length };
}

function openFenceAtEnd(state: EditorState): { char: string; length: number; indent: string } | null {
  const doc = state.doc;
  let open: { char: string; length: number; indent: string } | null = null;

  for (let lineNumber = 1; lineNumber <= doc.lines; lineNumber++) {
    const match = /^( {0,3})(`{3,}|~{3,})(.*)$/.exec(doc.line(lineNumber).text);
    if (!match) continue;
    const char = match[2][0];
    const length = match[2].length;
    if (!open) {
      open = { char, length, indent: match[1] };
      continue;
    }
    if (char === open.char && length >= open.length && !match[3].trim()) open = null;
  }

  return open;
}

export function parseMarkdownTable(source: string, startLine: number): ParsedTable | null {
  const lines = source.split(/\r?\n/);
  if (lines.length < 2) return null;

  const header = splitTableRow(lines[0]);
  const delimiters = splitTableRow(lines[1]);
  if (!header.length || header.length !== delimiters.length) return null;

  const align: ('left' | 'center' | 'right')[] = [];
  for (const cell of delimiters) {
    const value = cell.trim();
    if (!/^:?-{3,}:?$/.test(value)) return null;
    align.push(
      value.startsWith(':') && value.endsWith(':')
        ? 'center'
        : value.endsWith(':')
          ? 'right'
          : 'left'
    );
  }

  const rows: string[][] = [];
  const lineNumbers = [startLine, startLine + 1];
  for (let index = 2; index < lines.length; index++) {
    const text = lines[index];
    if (!text.trim()) continue;
    const cells = splitTableRow(text);
    rows.push(Array.from({ length: header.length }, (_value, cell) => cells[cell] ?? ''));
    lineNumbers.push(startLine + index);
  }

  return { source, header, rows, align, lineNumbers };
}

function splitTableRow(line: string): string[] {
  let text = line.trim();
  if (text.startsWith('|')) text = text.slice(1);
  if (text.endsWith('|') && !text.endsWith('\\|')) text = text.slice(0, -1);

  const cells: string[] = [];
  let current = '';
  for (let index = 0; index < text.length; index++) {
    const char = text[index];
    if (char === '\\' && text[index + 1] === '|') {
      current += '|';
      index++;
      continue;
    }
    if (char === '|') {
      cells.push(current.trim());
      current = '';
      continue;
    }
    current += char;
  }
  cells.push(current.trim());
  return cells;
}

function appendInlineMarkdown(parent: HTMLElement, text: string) {
  let last = 0;
  for (const match of text.matchAll(TABLE_INLINE)) {
    const index = match.index ?? 0;
    if (index > last) parent.append(document.createTextNode(text.slice(last, index)));
    const token = match[0];

    if (token.startsWith('`')) {
      const code = document.createElement('code');
      code.textContent = token.slice(1, -1);
      parent.append(code);
    } else if (token.startsWith('**') || token.startsWith('__')) {
      const strong = document.createElement('strong');
      appendInlineMarkdown(strong, token.slice(2, -2));
      parent.append(strong);
    } else if (token.startsWith('~~')) {
      const struck = document.createElement('s');
      appendInlineMarkdown(struck, token.slice(2, -2));
      parent.append(struck);
    } else if (token.startsWith('*') || token.startsWith('_')) {
      const emphasis = document.createElement('em');
      appendInlineMarkdown(emphasis, token.slice(1, -1));
      parent.append(emphasis);
    } else if (token.startsWith('[[')) {
      const inner = token.slice(2, -2);
      const pipe = inner.indexOf('|');
      const target = (pipe >= 0 ? inner.slice(0, pipe) : inner).replace(/#.*$/, '').trim();
      const label = pipe >= 0 ? inner.slice(pipe + 1).trim() : target;
      const link = document.createElement('span');
      link.className = 'cm-live-table-wikilink';
      link.textContent = label || target;
      parent.append(link);
    } else if (token.startsWith('[')) {
      const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(token);
      const href = link ? safeHref(link[2]) : null;
      if (link && href) {
        const anchor = document.createElement('a');
        anchor.href = href;
        anchor.rel = 'noreferrer';
        if (!href.startsWith('#') && !href.startsWith('/')) anchor.target = '_blank';
        appendInlineMarkdown(anchor, link[1]);
        parent.append(anchor);
      } else if (link) {
        appendInlineMarkdown(parent, link[1]);
      }
    }

    last = index + token.length;
  }
  if (last < text.length) parent.append(document.createTextNode(text.slice(last)));
}

function safeHref(value: string): string | null {
  const href = value.trim();
  if (!href) return null;
  if (/^(https?:|mailto:)/i.test(href) || href.startsWith('/') || href.startsWith('#')) return href;
  return null;
}

function resolveImage(src: string, options: LiveMarkdownOptions): string {
  if (options.resolveImage) return options.resolveImage(src);
  if (/^(https?:|data:|\/)/i.test(src)) return src;
  return fileHref(src);
}

class TableWidget extends WidgetType {
  readonly table: ParsedTable;
  readonly from: number;

  constructor(table: ParsedTable, from: number) {
    super();
    this.table = table;
    this.from = from;
  }

  eq(other: TableWidget): boolean {
    return other.from === this.from && other.table.source === this.table.source;
  }

  toDOM(view: EditorView): HTMLElement {
    const wrap = document.createElement('div');
    wrap.className = 'cm-live-table-wrap';
    wrap.title = 'Click a row to edit this table';

    const table = document.createElement('table');
    table.className = 'cm-live-table';

    const head = table.createTHead();
    const headRow = head.insertRow();
    headRow.dataset.line = String(this.table.lineNumbers[0] ?? 1);
    this.table.header.forEach((cell, index) => {
      const th = document.createElement('th');
      th.style.textAlign = this.table.align[index] ?? 'left';
      appendInlineMarkdown(th, cell);
      headRow.append(th);
    });

    const body = table.createTBody();
    this.table.rows.forEach((row, rowIndex) => {
      const tr = body.insertRow();
      tr.dataset.line = String(this.table.lineNumbers[rowIndex + 2] ?? this.table.lineNumbers[0] ?? 1);
      row.forEach((cell, index) => {
        const td = document.createElement('td');
        td.style.textAlign = this.table.align[index] ?? 'left';
        appendInlineMarkdown(td, cell);
        tr.append(td);
      });
    });

    wrap.append(table);
    wrap.addEventListener('mousedown', (event) => {
      const target = event.target;
      if (!(target instanceof Element)) return;
      const row = target.closest('[data-line]');
      const lineNumber = Number(row?.getAttribute('data-line') ?? this.table.lineNumbers[0] ?? 1);
      const line = view.state.doc.line(Math.min(Math.max(lineNumber, 1), view.state.doc.lines));
      event.preventDefault();
      event.stopPropagation();
      view.dispatch({ selection: { anchor: line.from }, scrollIntoView: true });
      view.focus();
    });

    return wrap;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

class ImageWidget extends WidgetType {
  readonly src: string;
  readonly alt: string;

  constructor(src: string, alt: string) {
    super();
    this.src = src;
    this.alt = alt;
  }

  eq(other: ImageWidget): boolean {
    return other.src === this.src && other.alt === this.alt;
  }

  toDOM(): HTMLElement {
    const image = document.createElement('img');
    image.src = this.src;
    image.alt = this.alt;
    image.loading = 'lazy';
    image.className = 'cm-live-image';
    return image;
  }

  ignoreEvent(): boolean {
    return true;
  }
}
