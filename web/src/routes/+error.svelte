<script lang="ts">
	import { page } from '$app/state';

	import { Button } from '$lib/components/ui/button/index.js';
</script>

<svelte:head><title>{page.status} · lewnotes</title></svelte:head>

<main class="flex min-h-dvh items-center justify-center px-6 py-16">
	<section class="w-full max-w-md space-y-4">
		<p class="text-muted-foreground text-xs tracking-[0.2em] uppercase">error {page.status}</p>
		<h1 class="text-3xl font-semibold tracking-tight">
			{page.error?.message ?? 'Something went wrong'}
		</h1>
		<p class="text-muted-foreground text-sm leading-relaxed">
			The vault or the server could not answer. Check the server log, then return to the notes
			list.
		</p>
		<Button href="/notes" class="w-fit">Back to notes</Button>
	</section>
</main>
