<script lang="ts">
	import { formatDate, noteHref } from '#lib/api';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<svelte:head><title>Notes · lewnotes</title></svelte:head>

<header class="mb-8 flex flex-wrap items-end justify-between gap-3">
	<div>
		<p class="text-xs tracking-[0.18em] text-muted uppercase">vault</p>
		<h1 class="mt-1 text-3xl font-semibold tracking-tight">All notes</h1>
	</div>
	<p class="text-sm text-muted">{data.total} note{data.total === 1 ? '' : 's'}</p>
</header>

{#if data.notes.length === 0}
	<section class="border-border-subtle bg-surface-raised/50 rounded-lg border p-8 text-center">
		<h2 class="text-lg font-medium">The vault is empty</h2>
		<p class="mx-auto mt-2 max-w-sm text-sm leading-relaxed text-muted">
			Add a markdown file to the vault or create a note through the API. The server assigns it an id on the
			next scan.
		</p>
	</section>
{:else}
	<ul class="divide-border-subtle border-border-subtle divide-y overflow-hidden rounded-lg border">
		{#each data.notes as note (note.id)}
			<li>
				<a class="hover:bg-surface-raised/70 block px-4 py-4 transition-colors sm:px-5" href={noteHref(note.path)}>
					<div class="flex items-baseline justify-between gap-4">
						<span class="truncate font-medium">{note.title}</span>
						<span class="shrink-0 text-xs text-muted">{formatDate(note.updated_at)}</span>
					</div>
					<p class="mt-1 truncate font-mono text-[11px] text-muted">{note.path}</p>
					{#if note.excerpt}
						<p class="mt-2 line-clamp-2 text-sm leading-relaxed text-neutral-400">{note.excerpt}</p>
					{/if}
					{#if note.tags.length}
						<ul class="mt-2 flex flex-wrap gap-1.5">
							{#each note.tags as tag (tag)}
								<li class="bg-surface-raised rounded px-1.5 py-0.5 text-[11px] text-muted">#{tag}</li>
							{/each}
						</ul>
					{/if}
				</a>
			</li>
		{/each}
	</ul>
{/if}
