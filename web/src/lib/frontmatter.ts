// Frontmatter is stored as YAML on disk, but the editor treats it as a small
// set of attributes. The body is kept byte-for-byte so writing a title never
// reformats prose.
import { parseDocument, type Document } from 'yaml';

export interface SplitContent {
	frontmatter: string;
	body: string;
	hasFrontmatter: boolean;
}

const FRONTMATTER = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/;

export function splitFrontmatter(content: string): SplitContent {
	const match = FRONTMATTER.exec(content);
	if (!match) return { frontmatter: '', body: content, hasFrontmatter: false };
	return {
		frontmatter: match[1] ?? '',
		body: content.slice(match[0].length),
		hasFrontmatter: true
	};
}

export function joinFrontmatter(frontmatter: string, body: string): string {
	const inner = frontmatter.trim();
	if (!inner) return body;
	return `---\n${inner}\n---\n${body}`;
}

export function parseFrontmatter(frontmatter: string): Record<string, unknown> {
	try {
		const value = parseDocument(frontmatter).toJS();
		return value && typeof value === 'object' && !Array.isArray(value)
			? (value as Record<string, unknown>)
			: {};
	} catch {
		return {};
	}
}

export function frontmatterTitle(frontmatter: string): string {
	const value = parseFrontmatter(frontmatter).title;
	return typeof value === 'string' ? value.trim() : '';
}

export function frontmatterId(frontmatter: string): string {
	const value = parseFrontmatter(frontmatter).id;
	if (value === undefined || value === null) return '';
	return String(value).trim();
}

export function ensureFrontmatterID(frontmatter: string, id: string): string {
	if (frontmatterId(frontmatter)) return frontmatter;
	const trimmed = frontmatter.trimEnd();
	return trimmed ? `${trimmed}\nid: ${id}` : `id: ${id}`;
}

export function frontmatterTags(frontmatter: string): string[] {
	const raw = parseFrontmatter(frontmatter).tags;
	let values: string[] = [];
	if (Array.isArray(raw)) {
		values = raw.filter((value): value is string => typeof value === 'string');
	} else if (typeof raw === 'string') {
		values = raw.split(/[\s,]+/);
	}
	const seen = new Set<string>();
	const tags: string[] = [];
	for (const value of values) {
		const tag = value.trim().replace(/^#+/, '');
		if (!tag || seen.has(tag)) continue;
		seen.add(tag);
		tags.push(tag);
	}
	return tags;
}

export function customProperties(frontmatter: string): Record<string, unknown> {
	const values = parseFrontmatter(frontmatter);
	const properties: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(values)) {
		if (key === 'id' || key === 'title' || key === 'tags') continue;
		properties[key] = value;
	}
	return properties;
}

export function setTitle(frontmatter: string, title: string): string {
	const value = title.trim();
	return mutate(frontmatter, (doc) => {
		if (value) doc.set('title', value);
		else doc.delete('title');
	});
}

export function setTags(frontmatter: string, tags: string[]): string {
	const seen = new Set<string>();
	const clean: string[] = [];
	for (const value of tags) {
		const tag = value.trim().replace(/^#+/, '');
		if (!tag || seen.has(tag)) continue;
		seen.add(tag);
		clean.push(tag);
	}
	return mutate(frontmatter, (doc) => {
		if (clean.length) doc.set('tags', clean);
		else doc.delete('tags');
	});
}

export function replaceCustomProperties(
	frontmatter: string,
	properties: Record<string, unknown>
): string {
	const existing = parseFrontmatter(frontmatter);
	return mutate(frontmatter, (doc) => {
		for (const key of Object.keys(existing)) {
			if (key === 'id' || key === 'title' || key === 'tags') continue;
			doc.delete(key);
		}
		for (const [key, value] of Object.entries(properties)) {
			if (!key.trim() || key === 'id' || key === 'title' || key === 'tags') continue;
			doc.set(key, value);
		}
	});
}

/** Content for a brand-new note: frontmatter title, no duplicate body H1. */
export function frontmatterForTitle(title: string): string {
	return joinFrontmatter(setTitle('', title), '\n');
}

function mutate(frontmatter: string, update: (doc: Document) => void): string {
	try {
		const doc = parseDocument(frontmatter);
		update(doc);
		return doc.toString({ lineWidth: 0 }).trimEnd();
	} catch {
		return frontmatter;
	}
}
