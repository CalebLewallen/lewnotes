<script lang="ts">
	import { formatDate, noteHref } from '#lib/api';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<svelte:head><title>Notes · lewnotes</title></svelte:head>

<header class="mb-8 flex flex-wrap items-end justify-between gap-3">
	<div>
		<p class="text-muted-foreground text-xs tracking-[0.18em] uppercase">vault</p>
		<h1 class="mt-1 text-3xl font-semibold tracking-tight">All notes</h1>
	</div>
	<p class="text-muted-foreground text-sm">{data.total} note{data.total === 1 ? '' : 's'}</p>
</header>

{#if data.notes.length === 0}
	<Empty.Root class="border-border bg-card/40 border">
		<Empty.Header>
			<Empty.Title>The vault is empty</Empty.Title>
			<Empty.Description>
				Add a markdown file to the vault or create a note through the API. The server assigns it
				an id on the next scan.
			</Empty.Description>
		</Empty.Header>
	</Empty.Root>
{:else}
	<Item.Group class="border-border gap-0 overflow-hidden rounded-lg border">
		{#each data.notes as note (note.id)}
			<Item.Root
				variant="default"
				class="border-border rounded-none border-x-0 border-t-0 last:border-b-0"
			>
				{#snippet child({ props })}
					<a {...props} href={noteHref(note.path)}>
						<Item.Content>
							<div class="flex items-baseline justify-between gap-4">
								<Item.Title>{note.title}</Item.Title>
								<span class="text-muted-foreground shrink-0 text-xs">{formatDate(note.updated_at)}</span>
							</div>
							<p class="text-muted-foreground truncate font-mono text-[11px]">{note.path}</p>
							{#if note.excerpt}
								<Item.Description class="line-clamp-2">{note.excerpt}</Item.Description>
							{/if}
							{#if note.tags.length}
								<div class="flex flex-wrap gap-1.5">
									{#each note.tags as tag (tag)}
										<Badge variant="secondary" class="text-[10px]">#{tag}</Badge>
									{/each}
								</div>
							{/if}
						</Item.Content>
					</a>
				{/snippet}
			</Item.Root>
		{/each}
	</Item.Group>
{/if}
