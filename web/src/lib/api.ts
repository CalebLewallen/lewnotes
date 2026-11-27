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

export interface CreateNoteInput {
	path: string;
	content?: string;
}

export interface MoveResult {
	moved: boolean;
	note: Note;
	links_rewritten: number;
	warnings?: string[];
}

export interface DirMoveResult {
	moved: boolean;
	path: string;
	notes_moved: number;
	links_rewritten: number;
	warnings?: string[];
}

export interface SearchHit {
	id: string;
	path: string;
	title: string;
	snippet: string;
	tags: string[];
	type: 'note' | 'todo' | 'attachment' | 'whiteboard';
}

export interface SearchResponse {
	hits: SearchHit[];
	total: number;
	next_cursor?: string;
}

export interface TokenView {
	id: string;
	name: string;
	prefix: string;
	scope: string;
	created_at: string;
	last_used_at?: string | null;
	user_agent?: string;
}

export interface IssuedToken extends TokenView {
	token: string;
}

export interface TokenList {
	tokens: TokenView[];
}

export interface InstanceSettings {
	public_url: string;
	source: 'config' | 'settings' | 'derived';
	derived_origin: string;
	trust_proxy: boolean;
	listen: string;
	instance_name?: string;
}

export interface ProbeResult {
	valid: boolean;
	url: string;
	version?: string;
	instance_name?: string;
	setup_required: boolean;
	latency_ms: number;
}

export interface CLILoginPreview {
	id: string;
	device_name: string;
	client: string;
	created_at: string;
	expires_at: string;
	server: string;
	status: string;
	approve_url?: string;
}

export interface CLILoginApproval {
	status: string;
	device_name: string;
	redirect_url?: string;
}

interface Problem {
	type?: string;
	title?: string;
	detail?: string;
}

export class ApiError extends Error {
	status: number;
	type?: string;
	etag?: string;

	constructor(status: number, message: string, options: { type?: string; etag?: string } = {}) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.type = options.type;
		this.etag = options.etag;
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
	if (init.body && !(init.body instanceof FormData)) headers.set('Content-Type', 'application/json');
	headers.set('X-Lewnotes-Client', 'web');
	if (csrfToken && method !== 'GET' && method !== 'HEAD') {
		headers.set('X-Lewnotes-CSRF', csrfToken);
	}

	const response = await fetchFn(path, { ...init, method, headers, credentials: 'include' });
	if (!response.ok) {
		let problem: Problem = {};
		let detail = `${response.status} ${response.statusText}`;
		try {
			problem = (await response.json()) as Problem;
			detail = problem.detail || problem.title || detail;
		} catch {
			// Non-JSON errors keep the status text.
		}
		if (response.status === 401) csrfToken = '';
		throw new ApiError(response.status, detail, {
			type: problem.type,
			etag: response.headers.get('ETag') ?? undefined
		});
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
	noteById(fetchFn: FetchFn, id: string) {
		return request<Note>(`/api/v1/notes/${encodeURIComponent(id)}`, {}, fetchFn);
	},
	createNote(fetchFn: FetchFn, input: CreateNoteInput) {
		return request<Note>(
			'/api/v1/notes',
			{ method: 'POST', body: JSON.stringify({ path: input.path, content: input.content ?? '' }) },
			fetchFn
		);
	},
	updateNote(fetchFn: FetchFn, id: string, content: string, etag: string) {
		return request<Note>(
			`/api/v1/notes/${encodeURIComponent(id)}`,
			{
				method: 'PUT',
				body: JSON.stringify({ content }),
				headers: { 'If-Match': etag }
			},
			fetchFn
		);
	},
	moveNote(fetchFn: FetchFn, id: string, path: string, rewriteLinks = true) {
		return request<MoveResult>(
			`/api/v1/notes/${encodeURIComponent(id)}/move`,
			{ method: 'POST', body: JSON.stringify({ path, rewrite_links: rewriteLinks }) },
			fetchFn
		);
	},
	createDir(fetchFn: FetchFn, path: string) {
		return request<{ path: string }>(
			'/api/v1/dirs',
			{ method: 'POST', body: JSON.stringify({ path }) },
			fetchFn
		);
	},
	moveDir(fetchFn: FetchFn, path: string, newPath: string, rewriteLinks = true) {
		return request<DirMoveResult>(
			'/api/v1/dirs/move',
			{ method: 'POST', body: JSON.stringify({ path, new_path: newPath, rewrite_links: rewriteLinks }) },
			fetchFn
		);
	},
	search(fetchFn: FetchFn, query: string, limit = 20) {
		const params = new URLSearchParams({ q: query, limit: String(limit) });
		return request<SearchResponse>(`/api/v1/search?${params}`, {}, fetchFn);
	},
	tree(fetchFn: FetchFn = fetch, depth = 6) {
		return request<TreeResponse>(`/api/v1/tree?depth=${depth}`, {}, fetchFn);
	},
	listTokens(fetchFn: FetchFn = fetch) {
		return request<TokenList>('/api/v1/tokens', {}, fetchFn);
	},
	createToken(fetchFn: FetchFn, name: string) {
		return request<IssuedToken>(
			'/api/v1/tokens',
			{ method: 'POST', body: JSON.stringify({ name }) },
			fetchFn
		);
	},
	revokeToken(fetchFn: FetchFn, id: string) {
		return request<void>(`/api/v1/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' }, fetchFn);
	},
	getSettings(fetchFn: FetchFn = fetch) {
		return request<InstanceSettings>('/api/v1/settings', {}, fetchFn);
	},
	updateSettings(fetchFn: FetchFn, publicUrl: string) {
		return request<InstanceSettings>(
			'/api/v1/settings',
			{ method: 'PATCH', body: JSON.stringify({ public_url: publicUrl }) },
			fetchFn
		);
	},
	validateServer(fetchFn: FetchFn, url: string) {
		return request<ProbeResult>(
			'/api/v1/settings/validate',
			{ method: 'POST', body: JSON.stringify({ url }) },
			fetchFn
		);
	},
	previewCLILogin(fetchFn: FetchFn, id: string) {
		return request<CLILoginPreview>(
			`/api/v1/cli/login/${encodeURIComponent(id)}/preview`,
			{},
			fetchFn
		);
	},
	approveCLILogin(fetchFn: FetchFn, id: string, deviceName: string) {
		return request<CLILoginApproval>(
			`/api/v1/cli/login/${encodeURIComponent(id)}/approve`,
			{ method: 'POST', body: JSON.stringify({ device_name: deviceName }) },
			fetchFn
		);
	},
	denyCLILogin(fetchFn: FetchFn, id: string) {
		return request<{ status: string }>(
			`/api/v1/cli/login/${encodeURIComponent(id)}/deny`,
			{ method: 'POST', body: JSON.stringify({}) },
			fetchFn
		);
	},
	resolveCLILogin(fetchFn: FetchFn, userCode: string) {
		return request<CLILoginPreview>(
			'/api/v1/cli/login/resolve',
			{ method: 'POST', body: JSON.stringify({ user_code: userCode }) },
			fetchFn
		);
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
