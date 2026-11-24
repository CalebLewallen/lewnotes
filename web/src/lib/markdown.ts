// Render a note body to sanitized HTML and turn wikilinks into navigation.
import DOMPurify from 'dompurify';
import { marked } from 'marked';

import { fileHref, noteHref, type Note, type NoteLink } from './api';

const IMAGE = /\.(png|jpe?g|gif|webp|svg|avif)$/i;
const WIKILINK = /(!?)\[\[([^\]\n]+)\]\]/g;

export function renderNote(note: Note): string {
	const links = new Map<string, NoteLink>();
	for (const link of note.links) {
		links.set(`${link.kind}:${link.target.toLowerCase()}`, link);
	}

	const body = note.body.replace(WIKILINK, (_match, bang: string, inner: string) => {
		const kind = bang ? 'embed' : 'link';
		const pipe = inner.indexOf('|');
		const targetPart = (pipe >= 0 ? inner.slice(0, pipe) : inner).trim();
		const alias = pipe >= 0 ? inner.slice(pipe + 1).trim() : '';
		const hash = targetPart.indexOf('#');
		const target = (hash >= 0 ? targetPart.slice(0, hash) : targetPart).trim();
		const fragment = hash >= 0 ? targetPart.slice(hash + 1).trim() : '';
		if (!target) return escapeHtml(inner);

		const link = links.get(`${kind}:${target.toLowerCase()}`);
		const display = alias || target;
		const resolved = link && (link.resolution === 'exact' || link.resolution === 'suffix');

		if (kind === 'embed' && IMAGE.test(target)) {
			// The resolver indexes notes; images resolve against the files API.
			const path = link?.path || target;
			return `<img src="${escapeAttr(fileHref(path))}" alt="${escapeAttr(display)}" loading="lazy" />`;
		}
		if (link && resolved) {
			const anchor = fragment ? `#${encodeURIComponent(fragment)}` : '';
			const suffix = kind === 'embed' ? ' data-embed="true"' : '';
			return `<a href="${escapeAttr(noteHref(link.path ?? target))}${anchor}"${suffix}>${escapeHtml(display)}</a>`;
		}
		if (kind === 'embed' && !IMAGE.test(target)) {
			return `<span class="wikilink unresolved" title="Unresolved embed">!${escapeHtml(display)}</span>`;
		}
		return `<span class="wikilink unresolved" title="Unresolved link">${escapeHtml(display)}</span>`;
	});

	const html = marked.parse(body) as string;
	return DOMPurify.sanitize(html, { ADD_ATTR: ['target'] });
}

function escapeHtml(value: string): string {
	return value.replace(/[&<>"']/g, (char) => {
		switch (char) {
			case '&':
				return '&amp;';
			case '<':
				return '&lt;';
			case '>':
				return '&gt;';
			case '"':
				return '&quot;';
			default:
				return '&#39;';
		}
	});
}

function escapeAttr(value: string): string {
	return escapeHtml(value);
}
