<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { tick } from 'svelte';

	import {
		api,
		ApiError,
		fileHref,
		noteHref,
		type SearchHit,
		type TreeNode
	} from '#lib/api';
	import { frontmatterForTitle } from '#lib/frontmatter';
	import { live } from '#lib/live.svelte.js';
	import { sanitizeSnippet } from '#lib/markdown';
	import {
		folderPathForName,
		INBOX_DIR,
		isInboxPath,
		notePathForName,
		notePathInDir,
		parentDir
	} from '#lib/paths';
	import { toast } from '#lib/toast';
	import { useVaultTree } from '#lib/vault-tree';
	import * as Command from '$lib/components/ui/command/index.js';
	import { Kbd } from '$lib/components/ui/kbd/index.js';

	interface Props {
		open?: boolean;
		onchanged?: () => void | Promise<void>;
		onsignout?: () => void | Promise<void>;
	}

	type CreateType = 'note' | 'folder';

	type Step =
		| { kind: 'root' }
		| { kind: 'create-parent'; type: CreateType }
		| { kind: 'create-name'; type: CreateType; parent: string; parentLabel: string }
		| { kind: 'rename-target' }
		| { kind: 'rename-name'; node: TreeNode };

	interface FlatNode {
		node: TreeNode;
		depth: number;
	}

	interface PaletteAction {
		id: string;
		label: string;
		description: string;
		keywords?: string;
		run: () => void;
	}

	interface ParentOption {
		path: string;
		label: string;
		description: string;
	}

	let { open = $bindable(false), onchanged, onsignout }: Props = $props();

	const getTree = useVaultTree();

	let query = $state('');
	let hits = $state<SearchHit[]>([]);
	let total = $state(0);
	let loading = $state(false);
	let error = $state('');
	let step = $state<Step>({ kind: 'root' });
	let actionError = $state('');
	let actionBusy = $state(false);
	let wasOpen = false;
	let sequence = 0;

	function flatten(nodes: TreeNode[], depth = 0, out: FlatNode[] = []): FlatNode[] {
		for (const node of nodes) {
			out.push({ node, depth });
			if (node.type === 'dir' && node.children?.length) flatten(node.children, depth + 1, out);
		}
		return out;
	}

	const treeItems = $derived(flatten(getTree()));

	function nodeLabel(node: TreeNode): string {
		return node.type === 'note' ? node.name.replace(/\.md$/i, '') : node.name;
	}

	const actions: PaletteAction[] = [
		{
			id: 'new-note',
			label: 'New note',
			description: 'Create a markdown note.',
			keywords: 'create add',
			run: () => startCreate('note')
		},
		{
			id: 'new-folder',
			label: 'New folder',
			description: 'Create a directory in the vault.',
			keywords: 'create add directory',
			run: () => startCreate('folder')
		},
		{
			id: 'rename',
			label: 'Rename',
			description: 'Rename a note or folder.',
			keywords: 'move title name',
			run: () => setStep({ kind: 'rename-target' })
		},
		{
			id: 'all-notes',
			label: 'All notes',
			description: 'Open the vault index.',
			keywords: 'browse index vault',
			run: openAllNotes
		},
		{
			id: 'sign-out',
			label: 'Sign out',
			description: 'End this session.',
			keywords: 'logout leave',
			run: signOut
		}
	];

	const filteredActions = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return actions;
		return actions.filter((action) =>
			`${action.label} ${action.description} ${action.keywords ?? ''}`.toLowerCase().includes(q)
		);
	});

	const parentOptions = $derived.by<ParentOption[]>(() => {
		const notesOnly = step.kind === 'create-parent' && step.type === 'note';
		// Notes are rooted at the inbox; folders keep the whole vault as their root.
		const options: ParentOption[] = notesOnly
			? [{ path: INBOX_DIR, label: 'Inbox', description: 'Root for notes' }]
			: [{ path: '', label: 'Vault root', description: 'Top level' }];
		for (const { node } of treeItems) {
			if (node.type !== 'dir') continue;
			if (notesOnly && (!isInboxPath(node.path) || node.path === INBOX_DIR)) continue;
			options.push({ path: node.path, label: node.name, description: node.path });
		}
		const q = query.trim().toLowerCase();
		return !q
			? options
			: options.filter((option) =>
					`${option.label} ${option.path}`.toLowerCase().includes(q)
				);
	});

	const renameTargets = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return treeItems.filter(({ node }) => {
			if (node.type !== 'note' && node.type !== 'dir') return false;
			if (!q) return true;
			return `${nodeLabel(node)} ${node.path}`.toLowerCase().includes(q);
		});
	});

	const createPath = $derived.by(() => {
		if (step.kind !== 'create-name') return '';
		const name = query.trim();
		if (!name) return '';
		return step.type === 'folder'
			? folderPathForName(name, step.parent)
			: notePathInDir(name, step.parent);
	});

	const renamePath = $derived.by(() => {
		if (step.kind !== 'rename-name') return '';
		const name = query.trim();
		if (!name) return '';
		if (step.node.type === 'dir') return folderPathForName(name, parentDir(step.node.path));
		if (step.node.type === 'note') return notePathForName(name, step.node.path);
		return '';
	});

	const inputPlaceholder = $derived.by(() => {
		if (step.kind === 'root') return 'Search notes or run a command…';
		if (step.kind === 'create-parent') return 'Choose a folder…';
		if (step.kind === 'create-name') return step.type === 'folder' ? 'Folder name…' : 'Note title…';
		if (step.kind === 'rename-target') return 'Find a note or folder…';
		return 'New name…';
	});

	const status = $derived.by(() => {
		if (actionBusy) return 'Working…';
		if (step.kind === 'root') {
			return query.trim()
				? `${total} result${total === 1 ? '' : 's'}`
				: 'Search notes or run a command';
		}
		if (step.kind === 'create-parent') {
			return `Choose where to create the ${step.type === 'folder' ? 'folder' : 'note'}`;
		}
		if (step.kind === 'create-name') return `Creating in ${step.parentLabel}`;
		if (step.kind === 'rename-target') return 'Choose a note or folder to rename';
		return step.node.path;
	});

	const enterHint = $derived.by(() => {
		if (step.kind === 'create-name') return 'create';
		if (step.kind === 'rename-name') return 'rename';
		if (step.kind === 'root') return 'run';
		return 'continue';
	});

	$effect(() => {
		if (open && !wasOpen) {
			step = { kind: 'root' };
			query = '';
			hits = [];
			total = 0;
			loading = false;
			error = '';
			actionError = '';
			actionBusy = false;
		}
		wasOpen = open;
	});

	$effect(() => {
		const q = query.trim();
		const revision = live.revision;
		void revision;
		if (!open || step.kind !== 'root' || !q) {
			sequence += 1;
			hits = [];
			total = 0;
			loading = false;
			error = '';
			return;
		}

		const ticket = ++sequence;
		const timer = setTimeout(async () => {
			loading = true;
			error = '';
			try {
				const result = await api.search(fetch, q, 20);
				if (ticket !== sequence) return;
				hits = result.hits;
				total = result.total;
			} catch (e) {
				if (ticket !== sequence) return;
				hits = [];
				total = 0;
				error = e instanceof ApiError ? e.message : 'Search failed.';
			} finally {
				if (ticket === sequence) loading = false;
			}
		}, 180);

		return () => {
			clearTimeout(timer);
			if (ticket === sequence) loading = false;
		};
	});

	function setStep(next: Step, nextQuery = ''): void {
		step = next;
		query = nextQuery;
		actionError = '';
		actionBusy = false;
		hits = [];
		total = 0;
		loading = false;
		error = '';
	}

	function focusName(select = false): void {
		void tick().then(() => {
			const input = document.querySelector<HTMLInputElement>('[data-slot="command-input"]');
			if (!input) return;
			input.focus();
			if (select) input.select();
		});
	}

	function startCreate(type: CreateType): void {
		setStep({ kind: 'create-parent', type });
	}

	function chooseParent(option: ParentOption): void {
		if (step.kind !== 'create-parent') return;
		const type = step.type;
		setStep({
			kind: 'create-name',
			type,
			parent: option.path,
			parentLabel: option.path || 'Vault root'
		});
		focusName();
	}

	function beginRename(node: TreeNode): void {
		if (node.type !== 'note' && node.type !== 'dir') return;
		const name = node.type === 'dir' ? node.name : node.name.replace(/\.md$/i, '');
		setStep({ kind: 'rename-name', node }, name);
		focusName(true);
	}

	function goBack(): void {
		if (step.kind === 'create-name') {
			setStep({ kind: 'create-parent', type: step.type });
		} else if (step.kind === 'rename-name') {
			setStep({ kind: 'rename-target' });
		} else if (step.kind === 'create-parent' || step.kind === 'rename-target') {
			setStep({ kind: 'root' });
		}
	}

	function navigate(hit: SearchHit): void {
		open = false;
		if (hit.type === 'note') {
			void goto(noteHref(hit.path));
		} else if (hit.type === 'attachment') {
			window.open(fileHref(hit.path), '_blank', 'noreferrer');
		}
	}

	function openAllNotes(): void {
		open = false;
		void goto('/notes');
	}

	async function signOut(): Promise<void> {
		open = false;
		await onsignout?.();
	}

	async function submitCreate(): Promise<void> {
		if (step.kind !== 'create-name' || actionBusy) return;
		const name = query.trim();
		if (!name) {
			actionError = 'Enter a name.';
			return;
		}

		actionBusy = true;
		actionError = '';
		try {
			if (step.type === 'folder') {
				const path = folderPathForName(name, step.parent);
				await api.createDir(fetch, path);
				open = false;
				toast(`Created folder ${path}.`);
				await onchanged?.();
				return;
			}

			const path = notePathInDir(name, step.parent);
			const created = await api.createNote(fetch, {
				path,
				content: frontmatterForTitle(name)
			});
			open = false;
			await onchanged?.();
			await goto(noteHref(created.path));
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not create it.';
		} finally {
			actionBusy = false;
		}
	}

	async function submitRename(): Promise<void> {
		if (step.kind !== 'rename-name' || actionBusy) return;
		const name = query.trim();
		if (!name) {
			actionError = 'Enter a name.';
			return;
		}

		const node = step.node;
		const oldPath = node.path;
		const newPath =
			node.type === 'dir'
				? folderPathForName(name, parentDir(oldPath))
				: notePathForName(name, oldPath);
		if (newPath === oldPath) {
			actionError = 'Enter a different name.';
			return;
		}

		if (node.type === 'note' && !node.id) {
			actionError = 'This file cannot be renamed here.';
			return;
		}

		actionBusy = true;
		actionError = '';
		try {
			if (node.type === 'dir') {
				const result = await api.moveDir(fetch, oldPath, newPath, true);
				const notes = result.notes_moved === 1 ? '1 note' : `${result.notes_moved} notes`;
				const links = result.links_rewritten
					? ` ${result.links_rewritten} note${result.links_rewritten === 1 ? '' : 's'} had links rewritten.`
					: '';
				open = false;
				toast(`Renamed to ${result.path}. ${notes} moved.${links}`);
				await onchanged?.();
				await navigateAfterDirMove(oldPath, result.path);
				return;
			}

			const result = await api.moveNote(fetch, node.id!, newPath, true);
			const links = result.links_rewritten
				? ` ${result.links_rewritten} note${result.links_rewritten === 1 ? '' : 's'} had links rewritten.`
				: '';
			open = false;
			toast(`Moved to ${result.note.path}.${links}`);
			await onchanged?.();
			if (page.params.path === oldPath) {
				await goto(noteHref(result.note.path), { replaceState: true });
			}
		} catch (e) {
			actionError = e instanceof ApiError ? e.message : 'Could not rename it.';
		} finally {
			actionBusy = false;
		}
	}

	async function navigateAfterDirMove(oldPath: string, newPath: string): Promise<void> {
		const current = page.params.path ?? '';
		if (current === oldPath) {
			await goto(noteHref(newPath), { replaceState: true });
		} else if (current.startsWith(oldPath + '/')) {
			await goto(noteHref(newPath + current.slice(oldPath.length)), { replaceState: true });
		}
	}
</script>

<Command.Dialog
	bind:open
	title="Command palette"
	description="Search notes and files, or run an action from the sidebar."
	shouldFilter={false}
	class="sm:max-w-2xl"
>
	<Command.Input
		bind:value={query}
		placeholder={inputPlaceholder}
		autocomplete="off"
		spellcheck="false"
	/>

	{#if actionError}
		<div class="text-destructive px-3 py-2 text-sm" role="alert">{actionError}</div>
	{/if}

	<Command.List class="max-h-96">
		{#if step.kind === 'root'}
			{#if filteredActions.length}
				<Command.Group heading="Actions">
					{#each filteredActions as action (action.id)}
						<Command.Item
							value={`action:${action.id}`}
							onSelect={action.run}
							class="items-start py-2.5"
						>
							<span class="flex min-w-0 flex-col">
								<span class="truncate font-medium">{action.label}</span>
								<span class="text-muted-foreground truncate text-xs">{action.description}</span>
							</span>
						</Command.Item>
					{/each}
				</Command.Group>
			{/if}

			{#if error}
				<div class="text-destructive px-2 py-3 text-sm" role="alert">{error}</div>
			{:else if query.trim()}
				{#if loading && hits.length === 0}
					<Command.Empty>Searching…</Command.Empty>
				{:else if hits.length > 0}
					<Command.Group heading="Results">
						{#each hits as hit (hit.type + ':' + hit.id)}
							<Command.Item
								value={hit.type + ':' + hit.id}
								onSelect={() => navigate(hit)}
								class="items-start py-2.5"
							>
								<div class="min-w-0 flex-1">
									<span class="flex items-baseline justify-between gap-3">
										<span class="truncate text-sm font-medium">{hit.title || hit.path}</span>
										<span class="text-muted-foreground shrink-0 text-[10px] tracking-wider uppercase"
											>{hit.type}</span
										>
									</span>
									<span class="text-muted-foreground mt-0.5 block truncate font-mono text-[11px]"
										>{hit.path}</span
									>
									<span
										class="search-snippet mt-1.5 line-clamp-2 block text-xs leading-relaxed text-neutral-400"
									>
										{@html sanitizeSnippet(hit.snippet || hit.path)}
									</span>
								</div>
							</Command.Item>
						{/each}
					</Command.Group>
				{:else if filteredActions.length === 0}
					<Command.Empty>No notes or commands match “{query.trim()}”.</Command.Empty>
				{/if}
			{/if}
		{:else if step.kind === 'create-parent'}
			<Command.Group heading="Create in">
				{#each parentOptions as option (option.path)}
					<Command.Item
						value={`parent:${option.path}`}
						onSelect={() => chooseParent(option)}
						class="items-start py-2.5"
					>
						<span class="flex min-w-0 flex-col">
							<span class="truncate font-medium">{option.label}</span>
							<span class="text-muted-foreground truncate font-mono text-[11px]"
								>{option.description}</span
							>
						</span>
					</Command.Item>
				{/each}
			</Command.Group>
			{#if parentOptions.length === 0}
				<p class="text-muted-foreground px-2 py-3 text-sm">No folders match “{query.trim()}”.</p>
			{/if}
			<Command.Item value="back" onSelect={goBack} class="text-muted-foreground">
				← Back
			</Command.Item>
		{:else if step.kind === 'create-name'}
			<Command.Group heading={step.type === 'folder' ? 'Folder name' : 'Note title'}>
				<Command.Item
					value="create"
					onSelect={submitCreate}
					class="items-start py-2.5"
				>
					<span class="flex min-w-0 flex-col">
						<span class="truncate font-medium">
							{actionBusy
								? 'Creating…'
								: `Create ${step.type === 'folder' ? 'folder' : 'note'}`}
						</span>
						<span class="text-muted-foreground truncate font-mono text-[11px]">
							{createPath || (query.trim() ? 'Enter a valid name.' : 'Type a name above.')}
						</span>
					</span>
				</Command.Item>
			</Command.Group>
			<Command.Item value="back" onSelect={goBack} class="text-muted-foreground">
				← Back
			</Command.Item>
		{:else if step.kind === 'rename-target'}
			<Command.Group heading="Rename">
				{#each renameTargets as { node } (node.path)}
					<Command.Item
						value={`rename:${node.type}:${node.path}`}
						onSelect={() => beginRename(node)}
						disabled={node.type === 'note' && !node.id}
						class="items-start py-2.5"
					>
						<span class="flex min-w-0 flex-col">
							<span class="truncate font-medium">{nodeLabel(node)}</span>
							<span class="text-muted-foreground truncate font-mono text-[11px]">
								{node.type === 'dir' ? 'Folder' : 'Note'} · {node.path}
							</span>
						</span>
					</Command.Item>
				{/each}
			</Command.Group>
			{#if renameTargets.length === 0}
				<p class="text-muted-foreground px-2 py-3 text-sm">
					No notes or folders match “{query.trim()}”.
				</p>
			{/if}
			<Command.Item value="back" onSelect={goBack} class="text-muted-foreground">
				← Back
			</Command.Item>
		{:else if step.kind === 'rename-name'}
			<Command.Group heading={step.node.type === 'dir' ? 'New folder name' : 'New note name'}>
				<Command.Item
					value="rename"
					onSelect={submitRename}
					class="items-start py-2.5"
				>
					<span class="flex min-w-0 flex-col">
						<span class="truncate font-medium">
							{actionBusy ? 'Renaming…' : `Rename to “${query.trim() || '…'}”`}
						</span>
						<span class="text-muted-foreground truncate font-mono text-[11px]">
							{renamePath || (query.trim() ? 'Enter a valid name.' : 'Type a new name above.')}
						</span>
					</span>
				</Command.Item>
			</Command.Group>
			<Command.Item value="back" onSelect={goBack} class="text-muted-foreground">
				← Back
			</Command.Item>
		{/if}
	</Command.List>

	<div class="text-muted-foreground flex items-center justify-between gap-3 px-3 py-2 text-[11px]">
		<span>{status}</span>
		<span class="flex items-center gap-1.5">
			<Kbd>↑</Kbd><Kbd>↓</Kbd> select
			<span class="mx-1">·</span>
			<Kbd>Enter</Kbd> {enterHint}
			<span class="mx-1">·</span>
			<Kbd>Esc</Kbd> close
		</span>
	</div>
</Command.Dialog>
