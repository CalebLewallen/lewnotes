<script lang="ts">
	import { onMount } from 'svelte';

	type Health = { ok: boolean; version: string; uptime_seconds: number };

	let health = $state<Health | null>(null);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			const res = await fetch('/api/v1/health');
			if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
			health = (await res.json()) as Health;
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	});
</script>

<main class="mx-auto flex min-h-dvh max-w-3xl flex-col justify-center gap-8 px-6 py-16">
	<header class="space-y-3">
		<p class="text-xs tracking-[0.2em] text-neutral-500 uppercase">self-hosted notes</p>
		<h1 class="text-4xl font-semibold tracking-tight">lewnotes</h1>
		<p class="max-w-prose text-neutral-400">
			Markdown notes, todo.txt tasks, and Excalidraw whiteboards on your own server. One vault, one
			owner, every interface over the same API.
		</p>
	</header>

	<section class="border-border-subtle bg-surface-raised rounded-lg border p-5 font-mono text-sm">
		{#if health}
			<p class="text-emerald-400">server online · v{health.version}</p>
			<p class="mt-1 text-neutral-500">uptime {Math.floor(health.uptime_seconds)}s</p>
		{:else if error}
			<p class="text-amber-400">server unreachable</p>
			<p class="mt-1 text-neutral-500">{error}</p>
		{:else}
			<p class="text-neutral-500">checking server…</p>
		{/if}
	</section>

	<p class="text-sm text-neutral-500">
		Scaffolding only. The build plan lives in <code class="text-neutral-300"
			>.agents/plans/2026-11-24-build-slices.md</code
		>.
	</p>
</main>
