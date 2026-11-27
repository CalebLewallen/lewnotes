<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import MoreHorizontalIcon from '@lucide/svelte/icons/more-horizontal';
	import XIcon from '@lucide/svelte/icons/x';
	import { onMount, tick, untrack } from 'svelte';

	import { api, ApiError, fileHref, noteHref, type TreeNode } from '#lib/api';
	import SearchPalette from '#lib/components/SearchPalette.svelte';
	import SidebarAutoClose from '#lib/components/SidebarAutoClose.svelte';
	import { frontmatterForTitle } from '#lib/frontmatter';
	import { connectLive, disconnectLive, live } from '#lib/live.svelte.js';
	import { folderPathForName, isAttachmentsPath, notePathForName, notePathInDir, parentDir } from '#lib/paths';
	import { toast } from '#lib/toast';
	import { provideVaultTree } from '#lib/vault-tree';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Kbd } from '$lib/components/ui/kbd/index.js';
	import * as Sidebar from '$lib/components/ui/sidebar/index.js';
	import { Toaster } from '$lib/components/ui/sonner/index.js';
	import type { LayoutData } from './$types';

	let { data, children }: { data: LayoutData; children: import('svelte').Snippet } = $props();

	let loggingOut = $state(false);
	let searchOpen = $state(false);
	let isMac = $state(false);
	let tree = $state<TreeNode[]>(untrack(() => data.tree));
	provideVaultTree(() => tree);
	let treeLoading = $state(false);
	let treeSequence = 0;
	let treeNonceSeen = 0;

	let creating = $state<{ parent: string; type: 'note' | 'folder'; depth: number } | null>(null);
	let createName = $state('');
	let createError = $state('');
	let createBusy = $state(false);
	let renaming = $state<{
		path: string;
		type: 'note' | 'dir';
		id?: string;
		name: string;
		depth: number;
	} | null>(null);
	let renameName = $state('');
	let renameError = $state('');
	let renameBusy = $state(false);
	let collapsed = $state<Set<string>>(new Set());
	let dragNode = $state<TreeNode | null>(null);
	let dropTarget = $state<string | null>(null);

	interface FlatNode {
		node: TreeNode;
		depth: number;
	}

	function flatten(nodes: TreeNode[], depth = 0, out: FlatNode[] = []): FlatNode[] {
		for (const node of nodes) {
			out.push({ node, depth });
			if (node.type === 'dir' && node.children?.length && !collapsed.has(node.path)) {
				flatten(node.children, depth + 1, out);
			}
		}
		return out;
	}

	function collectDirPaths(nodes: TreeNode[], out: string[] = []): string[] {
		for (const node of nodes) {
			if (node.type === 'dir') {
				out.push(node.path);
				if (node.children?.length) collectDirPaths(node.children, out);
			}
		}
		return out;
	}

	const items = $derived(flatten(tree));
	const dirPaths = $derived(collectDirPaths(tree));
	const allCollapsed = $derived(dirPaths.length > 0 && dirPaths.every((path) => collapsed.has(path)));
	const currentPath = $derived(page.params.path ?? '');
	const noteRoute = $derived(page.url.pathname.startsWith('/n/'));

	afterNavigate(() => {
		creating = null;
		renaming = null;
	});

	$effect(() => {
		connectLive();
		return () => disconnectLive();
	});

	$effect(() => {
		const nonce = live.treeNonce;
		if (!nonce || nonce === treeNonceSeen) return;
		treeNonceSeen = nonce;
		void refreshTree();
	});

	onMount(() => {
		const mac = /Mac|iPhone|iPad|iPod/.test(`${navigator.platform} ${navigator.userAgent}`);
		isMac = mac;
		const onKey = (event: KeyboardEvent) => {
			const overlayOpen = document.querySelector(
				'dialog[open], [data-slot="sheet-content"], [data-slot="dropdown-menu-content"]'
			);
			if (!searchOpen && overlayOpen) return;
			const paletteShortcut = mac
				? event.metaKey && !event.ctrlKey && !event.altKey
				: event.altKey && !event.ctrlKey && !event.metaKey;
			if (paletteShortcut && event.key.toLowerCase() === 'k') {
				event.preventDefault();
				searchOpen = !searchOpen;
				return;
			}
			if (event.key === '/' && !searchOpen && !isEditable(event.target)) {
				event.preventDefault();
				searchOpen = true;
				return;
			}
			if (event.key === 'Escape') {
				if (creating) cancelCreate();
				else if (renaming) cancelRename();
			}
		};
		window.addEventListener('keydown', onKey);
		return () => window.removeEventListener('keydown', onKey);
	});

	async function refreshTree() {
		const sequence = ++treeSequence;
		treeLoading = true;
		try {
			const result = await api.tree(fetch);
			if (sequence === treeSequence) tree = result.tree;
		} catch (error) {
			if (error instanceof ApiError && error.status === 401) await goto('/login');
		} finally {
			if (sequence === treeSequence) treeLoading = false;
		}
	}

	async function logout() {
		loggingOut = true;
		try {
			await api.logout(fetch);
		} finally {
			await goto('/login');
		}
	}

	function isEditable(target: EventTarget | null): boolean {
		if (!(target instanceof HTMLElement)) return false;
		return (
			target.tagName === 'INPUT' ||
			target.tagName === 'TEXTAREA' ||
			target.tagName === 'SELECT' ||
			target.isContentEditable
		);
	}

	function startCreate(parent: string, type: 'note' | 'folder', depth: number) {
		creating = { parent, type, depth };
		createName = '';
		createError = '';
		void tick().then(() => {
			document.querySelector<HTMLInputElement>('[data-inline-input]')?.focus();
		});
	}

	function cancelCreate() {
		creating = null;
		createName = '';
		createError = '';
	}

	async function submitCreate(event: SubmitEvent) {
		event.preventDefault();
		if (!creating || createBusy) return;
		const name = createName.trim();
		if (!name) {
			createError = 'Enter a name.';
			return;
		}
		createBusy = true;
		createError = '';
		try {
			if (creating.type === 'folder') {
				const path = folderPathForName(name, creating.parent);
				await api.createDir(fetch, path);
				creating = null;
				createName = '';
				toast(`Created folder ${path}.`);
				await refreshTree();
				return;
			}
			const path = notePathInDir(name, creating.parent);
			const created = await api.createNote(fetch, { path, content: frontmatterForTitle(name) });
			creating = null;
			createName = '';
			await refreshTree();
			await goto(noteHref(created.path));
		} catch (error) {
			createError = error instanceof ApiError ? error.message : 'Could not create it.';
		} finally {
			createBusy = false;
		}
	}

	function startRename(node: TreeNode, depth: number) {
		if (node.type !== 'note' && node.type !== 'dir') return;
		renaming = {
			path: node.path,
			type: node.type === 'dir' ? 'dir' : 'note',
			id: node.id,
			name: node.type === 'dir' ? node.name : node.name.replace(/\.md$/i, ''),
			depth
		};
		renameName = renaming.name;
		renameError = '';
		void tick().then(() => {
			document.querySelector<HTMLInputElement>('[data-inline-input]')?.focus();
		});
	}

	function cancelRename() {
		renaming = null;
		renameName = '';
		renameError = '';
	}

	function toggleFolder(path: string) {
		const next = new Set(collapsed);
		if (next.has(path)) next.delete(path);
		else next.add(path);
		collapsed = next;
	}

	function toggleAllFolders() {
		collapsed = allCollapsed ? new Set<string>() : new Set(dirPaths);
	}

	function canDropInto(node: TreeNode, targetDir: string): boolean {
		if (node.type !== 'dir' && node.type !== 'note') return false;
		if (node.path === targetDir || parentDir(node.path) === targetDir) return false;
		if (node.type === 'dir' && targetDir.startsWith(node.path + '/')) return false;
		if (isAttachmentsPath(targetDir) && !isAttachmentsPath(node.path)) return false;
		return true;
	}

	function startDrag(event: DragEvent, node: TreeNode) {
		dragNode = node;
		dropTarget = null;
		if (event.dataTransfer) {
			event.dataTransfer.effectAllowed = 'move';
			event.dataTransfer.setData('text/plain', node.path);
		}
	}

	function endDrag() {
		dragNode = null;
		dropTarget = null;
	}

	function dragOver(event: DragEvent, targetDir: string) {
		if (!dragNode || !canDropInto(dragNode, targetDir)) {
			if (dropTarget === targetDir) dropTarget = null;
			return;
		}
		event.preventDefault();
		if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
		if (dropTarget !== targetDir) dropTarget = targetDir;
	}

	function dragLeave(event: DragEvent, targetDir: string) {
		const current = event.currentTarget as HTMLElement | null;
		const related = event.relatedTarget as Node | null;
		if (related && current?.contains(related)) return;
		if (dropTarget === targetDir) dropTarget = null;
	}

	function drop(event: DragEvent, targetDir: string) {
		event.preventDefault();
		const node = dragNode;
		if (!node) return;
		const allowed = canDropInto(node, targetDir);
		const newPath = targetDir ? `${targetDir}/${node.name}` : node.name;
		endDrag();
		if (!allowed || newPath === node.path) return;
		void moveIntoTarget(node, targetDir);
	}

	async function moveIntoTarget(node: TreeNode, targetDir: string) {
		const newPath = targetDir ? `${targetDir}/${node.name}` : node.name;
		const where = targetDir || 'the vault root';
		try {
			if (node.type === 'dir') {
				const result = await api.moveDir(fetch, node.path, newPath, true);
				remapCollapsed(node.path, result.path);
				await refreshTree();
				await navigateAfterDirMove(node.path, result.path);
				toast(`Moved ${node.name} to ${where}.`);
				return;
			}
			if (node.type !== 'note' || !node.id) return;
			const result = await api.moveNote(fetch, node.id, newPath, true);
			const links = result.links_rewritten
				? ` ${result.links_rewritten} note${result.links_rewritten === 1 ? '' : 's'} had links rewritten.`
				: '';
			toast(`Moved ${node.name.replace(/\.md$/i, '')} to ${where}.${links}`);
			await refreshTree();
			if (currentPath === node.path) {
				await goto(noteHref(result.note.path), { replaceState: true });
			}
		} catch (error) {
			toast(error instanceof ApiError ? error.message : 'Could not move it.', 'error');
		}
	}

	function remapCollapsed(oldPath: string, newPath: string) {
		const affected = [...collapsed].filter(
			(path) => path === oldPath || path.startsWith(oldPath + '/')
		);
		if (!affected.length) return;
		const next = new Set(collapsed);
		for (const path of affected) {
			next.delete(path);
			next.add(newPath + path.slice(oldPath.length));
		}
		collapsed = next;
	}

	async function submitRename(event: SubmitEvent) {
		event.preventDefault();
		if (!renaming || renameBusy) return;
		const name = renameName.trim();
		if (!name) {
			renameError = 'Enter a name.';
			return;
		}
		renameBusy = true;
		renameError = '';
		const oldPath = renaming.path;
		const type = renaming.type;
		const id = renaming.id;
		try {
			if (type === 'dir') {
				const newPath = folderPathForName(name, parentDir(oldPath));
				if (newPath === oldPath) {
					renaming = null;
					return;
				}
				const result = await api.moveDir(fetch, oldPath, newPath, true);
				const notes = result.notes_moved === 1 ? '1 note' : `${result.notes_moved} notes`;
				const links = result.links_rewritten
					? ` ${result.links_rewritten} note${result.links_rewritten === 1 ? '' : 's'} had links rewritten.`
					: '';
				toast(`Renamed to ${result.path}. ${notes} moved.${links}`);
				renaming = null;
				await refreshTree();
				await navigateAfterDirMove(oldPath, result.path);
				return;
			}

			if (!id) {
				renameError = 'This file cannot be renamed here.';
				return;
			}
			const newPath = notePathForName(name, oldPath);
			if (newPath === oldPath) {
				renaming = null;
				return;
			}
			const result = await api.moveNote(fetch, id, newPath, true);
			const links = result.links_rewritten
				? ` ${result.links_rewritten} note${result.links_rewritten === 1 ? '' : 's'} had links rewritten.`
				: '';
			toast(`Moved to ${result.note.path}.${links}`);
			renaming = null;
			await refreshTree();
			if (currentPath === oldPath) {
				await goto(noteHref(result.note.path), { replaceState: true });
			}
		} catch (error) {
			renameError = error instanceof ApiError ? error.message : 'Could not rename it.';
		} finally {
			renameBusy = false;
		}
	}

	async function navigateAfterDirMove(oldPath: string, newPath: string) {
		const current = page.params.path ?? '';
		if (current === oldPath) {
			await goto(noteHref(newPath), { replaceState: true });
		} else if (current.startsWith(oldPath + '/')) {
			await goto(noteHref(newPath + current.slice(oldPath.length)), { replaceState: true });
		}
	}
</script>

<svelte:head><title>{data.me.instance_name || 'lewnotes'}</title></svelte:head>

{#snippet dirActions(node: TreeNode, depth: number)}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon-xs" aria-label={`Actions for ${node.name}`}>
					<MoreHorizontalIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class="w-44">
			{#if !isAttachmentsPath(node.path)}
				<DropdownMenu.Item onSelect={() => startCreate(node.path, 'note', depth + 1)}>
					New note
				</DropdownMenu.Item>
			{/if}
			<DropdownMenu.Item onSelect={() => startCreate(node.path, 'folder', depth + 1)}>
				New folder
			</DropdownMenu.Item>
			<DropdownMenu.Separator />
			<DropdownMenu.Item onSelect={() => startRename(node, depth)}>Rename</DropdownMenu.Item>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet noteActions(node: TreeNode, depth: number)}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon-xs" aria-label={`Actions for ${node.name}`}>
					<MoreHorizontalIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class="w-36">
			<DropdownMenu.Item onSelect={() => startRename(node, depth)}>Rename</DropdownMenu.Item>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#snippet createRow(depth: number)}
	<Sidebar.MenuItem class="px-1 pb-1" style="padding-left: {0.5 + depth * 0.75}rem">
		<form class="flex items-start gap-1" onsubmit={submitCreate}>
			<div class="min-w-0 flex-1">
				<Input
					data-inline-input
					class="h-7 text-sm"
					aria-label={creating?.type === 'folder' ? 'Folder name' : 'Note title'}
					bind:value={createName}
					placeholder={creating?.type === 'folder' ? 'Folder name' : 'Note title'}
					spellcheck="false"
					autocomplete="off"
				/>
				{#if createError}
					<p class="text-destructive mt-1 text-[11px]" role="alert">{createError}</p>
				{/if}
			</div>
			<Button size="xs" type="submit" disabled={createBusy}>Add</Button>
			<Button variant="ghost" size="icon-xs" type="button" aria-label="Cancel" onclick={cancelCreate}>
				<XIcon />
			</Button>
		</form>
	</Sidebar.MenuItem>
{/snippet}

{#snippet renameRow(depth: number)}
	<Sidebar.MenuItem class="px-1 pb-1" style="padding-left: {0.5 + depth * 0.75}rem">
		<form class="flex items-start gap-1" onsubmit={submitRename}>
			<div class="min-w-0 flex-1">
				<Input
					data-inline-input
					class="h-7 text-sm"
					aria-label="New name"
					bind:value={renameName}
					spellcheck="false"
					autocomplete="off"
				/>
				{#if renameError}
					<p class="text-destructive mt-1 text-[11px]" role="alert">{renameError}</p>
				{/if}
			</div>
			<Button size="xs" type="submit" disabled={renameBusy}>Rename</Button>
			<Button variant="ghost" size="icon-xs" type="button" aria-label="Cancel" onclick={cancelRename}>
				<XIcon />
			</Button>
		</form>
	</Sidebar.MenuItem>
{/snippet}

<Sidebar.Provider style="--sidebar-width: 18rem">
	<SidebarAutoClose />
	<Sidebar.Root collapsible="offcanvas">
		<Sidebar.Header class="border-sidebar-border border-b">
			<div class="flex items-start justify-between gap-2">
				<a class="min-w-0" href="/notes">
					<span class="block truncate font-semibold tracking-tight"
						>{data.me.instance_name || 'lewnotes'}</span
					>
					<span class="text-muted-foreground block text-xs">{data.me.username} · self-hosted</span>
				</a>
				<Sidebar.Trigger class="md:hidden" />
			</div>
		</Sidebar.Header>

		<div class="px-3 pt-3">
			<Button
				variant="outline"
				class="h-8 w-full min-w-0 justify-between px-2.5 text-xs font-normal"
				onclick={() => (searchOpen = true)}
			>
				<span>Search</span>
				<Kbd class="border border-border bg-muted text-foreground text-[10px]">
					{isMac ? '⌘K' : 'Alt K'}
				</Kbd>
			</Button>
		</div>

		<Sidebar.Content class="px-2 pb-4">
			{#if dirPaths.length}
				<div class="flex justify-end px-1 pb-1">
					<Button
						variant="ghost"
						size="xs"
						class="text-muted-foreground hover:text-foreground h-6 px-1.5 text-[11px] font-normal"
						onclick={toggleAllFolders}
					>
						{allCollapsed ? 'Expand all' : 'Collapse all'}
					</Button>
				</div>
			{/if}
			<Sidebar.Group class="p-0">
				<Sidebar.Menu class="gap-0">
					{#if creating?.parent === ''}
						{@render createRow(0)}
					{/if}
					{#each items as item (item.node.path)}
						{@const node = item.node}
						{#if node.type === 'dir'}
							{#if renaming?.path === node.path}
								{@render renameRow(item.depth)}
							{:else}
								<Sidebar.MenuItem
									class="group/row flex items-center gap-1 rounded-md py-1 pr-1 {dropTarget === node.path
										? 'bg-sidebar-accent'
										: ''}"
									style="padding-left: {0.5 + item.depth * 0.75}rem"
									draggable="true"
									ondragstart={(event) => startDrag(event, node)}
									ondragend={endDrag}
									ondragover={(event) => dragOver(event, node.path)}
									ondragleave={(event) => dragLeave(event, node.path)}
									ondrop={(event) => drop(event, node.path)}
								>
									<button
										type="button"
										class="flex min-w-0 flex-1 items-center gap-1 text-left"
										aria-expanded={node.children?.length ? !collapsed.has(node.path) : undefined}
										onclick={() => toggleFolder(node.path)}
									>
										{#if node.children?.length}
											{#if collapsed.has(node.path)}
												<ChevronRightIcon class="text-muted-foreground size-3 shrink-0" />
											{:else}
												<ChevronDownIcon class="text-muted-foreground size-3 shrink-0" />
											{/if}
										{:else}
											<span class="size-3 shrink-0"></span>
										{/if}
										<span
											class="text-muted-foreground min-w-0 flex-1 truncate text-[11px] font-medium tracking-[0.12em] uppercase"
										>
											{node.name}
										</span>
									</button>
									<div
										class="flex items-center gap-0.5 opacity-100 md:opacity-0 md:group-focus-within/row:opacity-100 md:group-hover/row:opacity-100"
									>
										{@render dirActions(node, item.depth)}
									</div>
								</Sidebar.MenuItem>
							{/if}
							{#if creating?.parent === node.path}
								{@render createRow(item.depth + 1)}
							{/if}
						{:else if node.type === 'note'}
							{#if renaming?.path === node.path}
								{@render renameRow(item.depth)}
							{:else}
								<Sidebar.MenuItem
									class="group/row flex items-center gap-1 pr-1 {dragNode?.path === node.path
										? 'opacity-50'
										: ''}"
									draggable="true"
									ondragstart={(event) => startDrag(event, node)}
									ondragend={endDrag}
								>
									<Sidebar.MenuButton
										class="min-w-0 flex-1"
										style="padding-left: {0.5 + item.depth * 0.75}rem"
									>
										{#snippet child({ props })}
											<a {...props} href={noteHref(node.path)} draggable="false">
												<span class="truncate">{node.name.replace(/\.md$/i, '')}</span>
											</a>
										{/snippet}
									</Sidebar.MenuButton>
									<div
										class="flex items-center opacity-100 md:opacity-0 md:group-focus-within/row:opacity-100 md:group-hover/row:opacity-100"
									>
										{@render noteActions(node, item.depth)}
									</div>
								</Sidebar.MenuItem>
							{/if}
						{:else}
							<Sidebar.MenuItem>
								<a
									class="text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm transition-colors"
									style="padding-left: {0.5 + item.depth * 0.75}rem"
									href={fileHref(node.path)}
									target="_blank"
									rel="noreferrer"
								>
									<span class="truncate">{node.name}</span>
									<span class="text-[10px] uppercase">{node.type}</span>
								</a>
							</Sidebar.MenuItem>
						{/if}
					{/each}
					{#if dragNode && parentDir(dragNode.path) !== ''}
						<Sidebar.MenuItem class="px-1 pt-1">
							<div
								role="group"
								aria-label="Drop here to move to the vault root"
								class="rounded-md border border-dashed px-2 py-1.5 text-center text-[11px] transition-colors {dropTarget ===
								''
									? 'border-sidebar-accent bg-sidebar-accent text-sidebar-accent-foreground'
									: 'border-sidebar-border text-muted-foreground'}"
								ondragover={(event) => dragOver(event, '')}
								ondragleave={(event) => dragLeave(event, '')}
								ondrop={(event) => drop(event, '')}
							>
								Move to vault root
							</div>
						</Sidebar.MenuItem>
					{/if}
				</Sidebar.Menu>
				{#if items.length === 0 && !creating}
					<p class="text-muted-foreground px-3 py-6 text-sm">The vault is empty.</p>
				{/if}
			</Sidebar.Group>
		</Sidebar.Content>

		<Sidebar.Footer class="border-sidebar-border border-t">
			<div class="text-muted-foreground flex items-center justify-between gap-2 text-xs">
				<div class="flex items-center gap-3">
					<a class="hover:text-foreground" href="/notes">All notes</a>
					<a class="hover:text-foreground" href="/settings/instance">Instance</a>
					<a class="hover:text-foreground" href="/settings/tokens">Tokens</a>
				</div>
				<span
					class="inline-flex items-center gap-1.5"
					title={live.connected ? 'Live updates connected' : 'Reconnecting'}
				>
					<span
						class="size-1.5 rounded-full {live.connected ? 'bg-emerald-400' : 'bg-amber-400'}"
					></span>
					<span>{treeLoading ? 'Syncing' : live.connected ? 'Live' : 'Offline'}</span>
				</span>
				<Button
					variant="ghost"
					size="xs"
					class="text-muted-foreground hover:text-foreground text-xs"
					onclick={logout}
					disabled={loggingOut}
				>
					{loggingOut ? 'Signing out…' : 'Sign out'}
				</Button>
			</div>
		</Sidebar.Footer>
	</Sidebar.Root>

	<Sidebar.Inset class="min-w-0">
		<header
			class="border-border bg-background/95 sticky top-0 z-30 flex items-center gap-2 border-b px-3 py-2 backdrop-blur md:hidden"
		>
			<Sidebar.Trigger />
			<a class="min-w-0 flex-1 truncate font-semibold tracking-tight" href="/notes">
				{data.me.instance_name || 'lewnotes'}
			</a>
			<Button variant="ghost" size="sm" onclick={() => (searchOpen = true)}>Search</Button>
		</header>

		<div
			class={noteRoute
				? 'mx-auto max-w-4xl px-4 py-6 sm:px-10 sm:py-10'
				: 'mx-auto max-w-3xl px-5 py-8 sm:px-8 sm:py-12'}
		>
			{@render children()}
		</div>
	</Sidebar.Inset>
</Sidebar.Provider>

<SearchPalette bind:open={searchOpen} onchanged={refreshTree} onsignout={logout} />
<Toaster theme="dark" position="bottom-right" richColors closeButton />
