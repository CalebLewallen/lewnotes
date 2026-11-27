const IMAGE = /\.(png|jpe?g|gif|webp|svg|avif)$/i;

export function isImageTarget(target: string): boolean {
	return IMAGE.test(target);
}

/** Derive a creatable note path from an unresolved wikilink target. */
export function ghostNotePath(target: string, sourcePath: string): string {
	let clean = target.trim().replaceAll('\\', '/').replace(/^\/+/, '');
	if (!clean) return '';
	if (!/\.md$/i.test(clean)) clean += '.md';
	if (clean.includes('/')) return clean;

	const dir = sourcePath.split('/').slice(0, -1).join('/');
	return dir ? `${dir}/${clean}` : clean;
}

/** Human title for a note path, without the markdown extension. */
export function titleFromPath(path: string): string {
	const base = path.split('/').at(-1) ?? path;
	return base.replace(/\.md$/i, '') || 'Untitled';
}

export function isNotePath(path: string): boolean {
	return /\.md$/i.test(path);
}

export function parentDir(path: string): string {
	return path.split('/').slice(0, -1).join('/');
}

/** A safe filename that still reads like the title the user typed. */
export function sanitizeName(value: string): string {
	const clean = value
		.trim()
		.replace(/[\\/:*?"<>|\u0000-\u001f]/g, '-')
		.replace(/\s+/g, ' ')
		.replace(/^[.\s]+|[.\s]+$/g, '');
	return clean || 'Untitled';
}

export function noteFilename(title: string): string {
	const base = sanitizeName(title);
	return /\.md$/i.test(base) ? base : `${base}.md`;
}

export function folderName(name: string): string {
	return sanitizeName(name);
}

/** Build a note path from a title, preserving the current directory. */
export function notePathForTitle(title: string, sourcePath: string): string {
	const dir = parentDir(sourcePath);
	const file = noteFilename(title);
	return dir ? `${dir}/${file}` : file;
}

/** Build a note path from an explicit filename, preserving its directory. */
export function notePathForName(name: string, sourcePath: string): string {
	const dir = parentDir(sourcePath);
	const file = noteFilename(name);
	return dir ? `${dir}/${file}` : file;
}

/** Build a note path from a title in an explicit directory. */
export function notePathInDir(title: string, dir: string): string {
	const file = noteFilename(title);
	return dir ? `${dir}/${file}` : file;
}

/** Root folder for notes. */
export const INBOX_DIR = 'inbox';

/** Whether a path is the inbox itself or a folder inside it. */
export function isInboxPath(path: string): boolean {
	return path === INBOX_DIR || path.startsWith(`${INBOX_DIR}/`);
}

/** System folder for uploaded files; notes are not allowed inside it. */
export const ATTACHMENTS_DIR = 'attachments';

export function isAttachmentsPath(path: string): boolean {
	return path === ATTACHMENTS_DIR || path.startsWith(`${ATTACHMENTS_DIR}/`);
}

/** Build a folder path from a name in an explicit parent directory. */
export function folderPathForName(name: string, parent: string): string {
	const folder = folderName(name);
	return parent ? `${parent}/${folder}` : folder;
}
