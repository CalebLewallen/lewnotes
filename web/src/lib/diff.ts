export interface DiffLine {
	kind: 'same' | 'add' | 'del';
	text: string;
}

const MAX_CELLS = 4_000_000;

/**
 * Line diff using an LCS table after trimming the common edges. Notes are
 * prose-sized; the table cap falls back to a replace block for huge files.
 */
export function lineDiff(before: string, after: string): DiffLine[] {
	const a = before.split('\n');
	const b = after.split('\n');
	let prefix = 0;
	while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) prefix++;

	let suffix = 0;
	while (
		suffix < a.length - prefix &&
		suffix < b.length - prefix &&
		a[a.length - 1 - suffix] === b[b.length - 1 - suffix]
	) {
		suffix++;
	}

	const midA = a.slice(prefix, a.length - suffix);
	const midB = b.slice(prefix, b.length - suffix);
	const out: DiffLine[] = [];
	for (let i = 0; i < prefix; i++) out.push({ kind: 'same', text: a[i] });

	if (midA.length * midB.length > MAX_CELLS) {
		for (const text of midA) out.push({ kind: 'del', text });
		for (const text of midB) out.push({ kind: 'add', text });
	} else {
		out.push(...lcsDiff(midA, midB));
	}

	for (let i = a.length - suffix; i < a.length; i++) {
		out.push({ kind: 'same', text: a[i] });
	}
	return out;
}

function lcsDiff(a: string[], b: string[]): DiffLine[] {
	const rows = a.length + 1;
	const cols = b.length + 1;
	const table = Array.from({ length: rows }, () => new Uint32Array(cols));
	for (let i = a.length - 1; i >= 0; i--) {
		for (let j = b.length - 1; j >= 0; j--) {
			table[i][j] =
				a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
		}
	}

	const out: DiffLine[] = [];
	let i = 0;
	let j = 0;
	while (i < a.length && j < b.length) {
		if (a[i] === b[j]) {
			out.push({ kind: 'same', text: a[i] });
			i++;
			j++;
		} else if (table[i + 1][j] >= table[i][j + 1]) {
			out.push({ kind: 'del', text: a[i] });
			i++;
		} else {
			out.push({ kind: 'add', text: b[j] });
			j++;
		}
	}
	while (i < a.length) out.push({ kind: 'del', text: a[i++] });
	while (j < b.length) out.push({ kind: 'add', text: b[j++] });
	return out;
}
