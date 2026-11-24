// Single fetch wrapper for the lewnotes API. Cookie sessions are same-origin;
// mutations add the CSRF token the server put in the readable cookie.

export interface AuthStatus {
	setup_required: boolean;
	instance_name?: string;
	version?: string;
}

export interface Me {
	username: string;
	auth: 'session' | 'token';
	csrf_token?: string;
	instance_name?: string;
}

export interface NoteLink {
	id?: string;
	path?: string;
	title?: string;
	target: string;
	alias?: string;
	fragment?: string;
	kind: 'link' | 'embed';
	resolution: 'exact' | 'suffix' | 'unresolved' | 'ambiguous';
	line: number;
	candidates?: string[];
}

export interface Backlink {
	id: string;
	path: string;
	title: string;
	alias?: string;
	fragment?: string;
	kind: 'link' | 'embed';
	line: number;
}

export interface Note {
	id: string;
	path: string;
	title: string;
	etag: string;
	size: number;
	updated_at: string;
	tags: string[];
	frontmatter: Record<string, unknown>;
	content?: string;
	body: string;
	links: NoteLink[];
	backlinks: Backlink[];
	warnings?: string[];
}

export interface NoteSummary {
	id: string;
	path: string;
	title: string;
	excerpt: string;
	tags: string[];
	size: number;
	updated_at: number;
}

export interface NoteList {
	notes: NoteSummary[];
	total: number;
	next_cursor?: string;
}

export interface TreeNode {
	name: string;
	path: string;
	type: 'dir' | 'note' | 'board' | 'image' | 'file';
	id?: string;
	size?: number;
	modified?: string;
	children?: TreeNode[];
}

export interface TreeResponse {
	tree: TreeNode[];
}

export class ApiError extends Error {
	status: number;

	constructor(status: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
}

type FetchFn = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

let csrfToken = '';

export function setCSRF(token: string | undefined): void {
	csrfToken = token ?? '';
}

async function request<T>(
	path: string,
	init: RequestInit = {},
	fetchFn: FetchFn = fetch
): Promise<T> {
	const method = (init.method ?? 'GET').toUpperCase();
	const headers = new Headers(init.headers);
	if (init.body) headers.set('Content-Type', 'application/json');
	if (csrfToken && method !== 'GET' && method !== 'HEAD') {
		headers.set('X-Lewnotes-CSRF', csrfToken);
	}

	const response = await fetchFn(path, { ...init, method, headers, credentials: 'include' });
	if (!response.ok) {
		let detail = `${response.status} ${response.statusText}`;
		try {
			const problem = (await response.json()) as { detail?: string; title?: string };
			detail = problem.detail || problem.title || detail;
		} catch {
			// Non-JSON errors keep the status text.
		}
		if (response.status === 401) csrfToken = '';
		throw new ApiError(response.status, detail);
	}
	if (response.status === 204) return undefined as T;
	return (await response.json()) as T;
}

export const api = {
	authStatus(fetchFn: FetchFn = fetch) {
		return request<AuthStatus>('/api/v1/auth/status', {}, fetchFn);
	},
	me(fetchFn: FetchFn = fetch) {
		return request<Me>('/api/v1/auth/me', {}, fetchFn);
	},
	async setup(fetchFn: FetchFn, password: string, instanceName: string) {
		const me = await request<Me>(
			'/api/v1/auth/setup',
			{ method: 'POST', body: JSON.stringify({ password, instance_name: instanceName }) },
			fetchFn
		);
		setCSRF(me.csrf_token);
		return me;
	},
	async login(fetchFn: FetchFn, password: string) {
		const me = await request<Me>(
			'/api/v1/auth/login',
			{ method: 'POST', body: JSON.stringify({ password }) },
			fetchFn
		);
		setCSRF(me.csrf_token);
		return me;
	},
	logout(fetchFn: FetchFn = fetch) {
		return request<void>('/api/v1/auth/logout', { method: 'POST' }, fetchFn);
	},
	listNotes(fetchFn: FetchFn = fetch, params: Record<string, string> = {}) {
		const query = new URLSearchParams(params).toString();
		return request<NoteList>(`/api/v1/notes${query ? `?${query}` : ''}`, {}, fetchFn);
	},
	noteByPath(fetchFn: FetchFn, path: string) {
		return request<Note>(`/api/v1/notes/by-path?path=${encodeURIComponent(path)}`, {}, fetchFn);
	},
	tree(fetchFn: FetchFn = fetch, depth = 6) {
		return request<TreeResponse>(`/api/v1/tree?depth=${depth}`, {}, fetchFn);
	}
};

export function noteHref(path: string): string {
	return '/n/' + path.split('/').map(encodeURIComponent).join('/');
}

export function fileHref(path: string): string {
	return '/api/v1/files/' + path.split('/').map(encodeURIComponent).join('/');
}

export function formatDate(value: string | number): string {
	const date = typeof value === 'number' ? new Date(value * 1000) : new Date(value);
	if (Number.isNaN(date.getTime())) return '';
	return date.toLocaleDateString(undefined, {
		year: 'numeric',
		month: 'short',
		day: 'numeric'
	});
}
