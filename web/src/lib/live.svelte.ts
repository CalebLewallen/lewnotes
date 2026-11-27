// Shared SSE state. The app shell owns the connection; routes react to the
// event sequence and refetch only what they need.

export interface LiveEvent {
	type: string;
	id?: string;
	path?: string;
	new_path?: string;
	title?: string;
	etag?: string;
	origin?: string;
	reason?: string;
}

export const live = $state({
	connected: false,
	revision: 0,
	treeNonce: 0,
	last: null as LiveEvent | null
});

const EVENT_TYPES = [
	'note.created',
	'note.updated',
	'note.moved',
	'note.deleted',
	'tree.changed',
	'index.scan.started',
	'index.scan.progress',
	'index.scan.finished',
	'resync'
];

let source: EventSource | null = null;
let retryTimer: ReturnType<typeof setTimeout> | null = null;

export function connectLive(): void {
	if (source || typeof window === 'undefined') return;
	source = new EventSource('/api/v1/events');
	source.onopen = () => {
		live.connected = true;
	};
	source.onerror = () => {
		live.connected = false;
		if (source?.readyState === EventSource.CLOSED) scheduleReconnect();
	};
	source.onmessage = (event) => handle(event);

	for (const type of EVENT_TYPES) {
		source.addEventListener(type, handle as EventListener);
	}
}

export function disconnectLive(): void {
	source?.close();
	source = null;
	live.connected = false;
	if (retryTimer) {
		clearTimeout(retryTimer);
		retryTimer = null;
	}
}

function scheduleReconnect(): void {
	if (retryTimer) return;
	retryTimer = setTimeout(() => {
		retryTimer = null;
		source?.close();
		source = null;
		connectLive();
	}, 3000);
}

function handle(event: Event): void {
	const message = event as MessageEvent<string>;
	let data: Omit<LiveEvent, 'type'> = {};
	try {
		data = JSON.parse(message.data) as Omit<LiveEvent, 'type'>;
	} catch {
		// Ignore malformed frames; the next event or resync still works.
	}
	const type = message.type || 'message';
	live.last = { type, ...data };
	live.revision++;

	if (
		type === 'resync' ||
		type === 'index.scan.finished' ||
		type === 'note.created' ||
		type === 'note.moved' ||
		type === 'note.deleted' ||
		type === 'tree.changed' ||
		(type === 'note.updated' && data.origin !== 'web')
	) {
		live.treeNonce++;
	}
}
