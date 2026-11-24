<script lang="ts">
	import { noteHref, formatDate } from '#lib/api';
	import { renderNote } from '#lib/markdown';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const html = $derived(renderNote(data.note));
	const unresolved = $derived(
		data.note.links.filter((link) => link.resolution === 'unresolved' || link.resolution === 'ambiguous')
	);
</script>

<svelte:head><title>{data.note.title} · lewnotes</title></svelte:head>

<article>
	<header class="border-border-subtle mb-8 border-b pb-6">
		<p class="truncate font-mono text-[11px] text-muted">{data.note.path}</p>
		<h1 class="mt-2 text-3xl font-semibold tracking-tight sm:text-4xl">{data.note.title}</h1>
		<div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted">
			<span>Updated {formatDate(data.note.updated_at)}</span>
			<span>{data.note.links.length} link{data.note.links.length === 1 ? '' : 's'}</span>
			<span>{data.note.backlinks.length} backlink{data.note.backlinks.length === 1 ? '' : 's'}</span>
			{#if data.note.tags.length}
				<ul class="flex flex-wrap gap-1.5">
					{#each data.note.tags as tag (tag)}
						<li class="bg-surface-raised rounded px-1.5 py-0.5">#{tag}</li>
					{/each}
				</ul>
			{/if}
		</div>
	</header>

	<div class="note-body">
		<!-- Sanitized by DOMPurify in renderNote. -->
		{@html html}
	</div>

	{#if unresolved.length}
		<section class="border-border-subtle mt-10 border-t pt-6">
			<h2 class="text-xs font-medium tracking-[0.14em] text-muted uppercase">Unresolved links</h2>
			<ul class="mt-3 flex flex-wrap gap-2">
				{#each unresolved as link (`${link.kind}:${link.target}:${link.line}`)}
					<li class="border-border-subtle rounded border px-2 py-1 font-mono text-xs text-neutral-400">
						{link.kind === 'embed' ? '!' : ''}{link.target}{#if link.fragment}#{link.fragment}{/if}
						<span class="ml-1 text-[10px] uppercase text-muted">{link.resolution}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	{#if data.note.backlinks.length}
		<section class="border-border-subtle mt-10 border-t pt-6">
			<h2 class="text-xs font-medium tracking-[0.14em] text-muted uppercase">Linked from</h2>
			<ul class="mt-3 space-y-1">
				{#each data.note.backlinks as link (link.id + ':' + link.line)}
					<li>
						<a class="text-sm text-neutral-300 underline decoration-neutral-700 underline-offset-4 hover:text-neutral-100" href={noteHref(link.path)}>
							{link.title || link.path}
						</a>
						{#if link.alias}
							<span class="ml-2 text-xs text-muted">as “{link.alias}”</span>
						{:else if link.fragment}
							<span class="ml-2 text-xs text-muted">#{link.fragment}</span>
						{/if}
						<span class="ml-2 font-mono text-[10px] text-muted">line {link.line}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</article>
