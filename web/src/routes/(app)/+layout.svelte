<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';

	import { api, fileHref, noteHref, type TreeNode } from '#lib/api';
	import type { LayoutData } from './$types';

	let { data, children }: { data: LayoutData; children: import('svelte').Snippet } = $props();

	let mobileOpen = $state(false);
	let filter = $state('');
	let loggingOut = $state(false);

	interface FlatNode {
		node: TreeNode;
		depth: number;
	}

	function flatten(nodes: TreeNode[], depth = 0, out: FlatNode[] = []): FlatNode[] {
		for (const node of nodes) {
			out.push({ node, depth });
			if (node.type === 'dir' && node.children?.length) flatten(node.children, depth + 1, out);
		}
		return out;
	}

	const items = $derived(flatten(data.tree));
	const visible = $derived(
		filter.trim()
			? items.filter(({ node }) => node.path.toLowerCase().includes(filter.trim().toLowerCase()))
			: items
	);
	const currentPath = $derived(page.params.path ?? '');

	afterNavigate(() => {
		mobileOpen = false;
	});

	async function logout() {
		loggingOut = true;
		try {
			await api.logout(fetch);
		} finally {
			await goto('/login');
		}
	}
</script>

<svelte:head><title>{data.me.instance_name || 'lewnotes'}</title></svelte:head>

<div class="min-h-dvh md:flex">
	<header
		class="border-border-subtle bg-surface/95 sticky top-0 z-30 flex items-center gap-3 border-b px-4 py-3 backdrop-blur md:hidden"
	>
		<button
			class="btn-ghost px-2"
			type="button"
			aria-label="Open note list"
			aria-expanded={mobileOpen}
			onclick={() => (mobileOpen = true)}
		>
			<span class="text-lg">☰</span>
		</button>
		<a class="font-semibold tracking-tight" href="/notes">{data.me.instance_name || 'lewnotes'}</a>
	</header>

	{#if mobileOpen}
		<button
			class="fixed inset-0 z-30 bg-black/60 md:hidden"
			type="button"
			aria-label="Close note list"
			onclick={() => (mobileOpen = false)}
		></button>
	{/if}

	<aside
		class="border-border-subtle bg-surface-raised/40 fixed inset-y-0 left-0 z-40 flex w-72 max-w-[85vw] flex-col border-r transition-transform duration-200 md:sticky md:top-0 md:h-dvh md:translate-x-0 {mobileOpen
			? 'translate-x-0'
			: '-translate-x-full'}"
	>
		<div class="border-border-subtle flex items-center justify-between gap-2 border-b px-4 py-4">
			<a class="min-w-0" href="/notes">
				<span class="block truncate font-semibold tracking-tight">{data.me.instance_name || 'lewnotes'}</span>
				<span class="block text-xs text-muted">{data.me.username} · self-hosted</span>
			</a>
			<button
				class="btn-ghost px-2 text-lg md:hidden"
				type="button"
				aria-label="Close note list"
				onclick={() => (mobileOpen = false)}>×</button
			>
		</div>

		<div class="px-3 py-3">
			<input class="field py-1.5 text-sm" placeholder="Filter notes…" bind:value={filter} />
		</div>

		<nav class="min-h-0 flex-1 overflow-y-auto px-2 pb-4" aria-label="Vault">
			<ul class="space-y-0.5">
				{#each visible as item (item.node.path)}
					{@const node = item.node}
					{#if node.type === 'dir'}
						<li
							class="pt-3 pb-1 text-[11px] font-medium tracking-[0.12em] text-muted uppercase"
							style="padding-left: {0.5 + item.depth * 0.75}rem"
						>
							{node.name}
						</li>
					{:else if node.type === 'note'}
						<li>
							<a
								class="tree-link {currentPath === node.path ? 'tree-link-active' : ''}"
								style="padding-left: {0.5 + item.depth * 0.75}rem"
								href={noteHref(node.path)}
							>
								<span class="truncate">{node.name.replace(/\.md$/i, '')}</span>
							</a>
						</li>
					{:else}
						<li>
							<a
								class="tree-link text-muted"
								style="padding-left: {0.5 + item.depth * 0.75}rem"
								href={fileHref(node.path)}
								target="_blank"
								rel="noreferrer"
							>
								<span class="truncate">{node.name}</span>
								<span class="text-[10px] uppercase">{node.type}</span>
							</a>
						</li>
					{/if}
				{/each}
			</ul>
			{#if visible.length === 0}
				<p class="px-3 py-6 text-sm text-muted">No files match “{filter}”.</p>
			{/if}
		</nav>

		<div class="border-border-subtle flex items-center justify-between border-t px-4 py-3 text-xs text-muted">
			<a class="hover:text-neutral-200" href="/notes">All notes</a>
			<button class="hover:text-neutral-200 disabled:opacity-50" type="button" onclick={logout} disabled={loggingOut}>
				{loggingOut ? 'Signing out…' : 'Sign out'}
			</button>
		</div>
	</aside>

	<main class="min-w-0 flex-1">
		<div class="mx-auto max-w-3xl px-5 py-8 sm:px-8 sm:py-12">
			{@render children()}
		</div>
	</main>
</div>
