<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';

	import { api, ApiError, setCSRF } from '#lib/api';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';

	let code = $state('');
	let busy = $state(false);
	let checking = $state(true);
	let error = $state('');

	onMount(async () => {
		try {
			const me = await api.me(fetch);
			setCSRF(me.csrf_token);
		} catch {
			await goto('/login?next=/cli');
			return;
		}
		checking = false;
		const fromQuery = page.url.searchParams.get('code');
		if (fromQuery) {
			code = fromQuery;
			await resolve();
		}
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		await resolve();
	}

	async function resolve() {
		if (busy || !code.trim()) return;
		busy = true;
		error = '';
		try {
			const preview = await api.resolveCLILogin(fetch, code.trim());
			await goto(`/cli/authorize?id=${encodeURIComponent(preview.id)}`);
		} catch (e) {
			error =
				e instanceof ApiError ? e.message : 'Could not look up that code. Check it and try again.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Connect a device · lewnotes</title></svelte:head>

<main class="flex min-h-dvh items-center justify-center px-6 py-16">
	<section class="w-full max-w-md space-y-8">
		<header class="space-y-2">
			<p class="text-muted-foreground text-xs tracking-[0.2em] uppercase">cli login</p>
			<h1 class="text-3xl font-semibold tracking-tight">Connect a device</h1>
			<p class="text-muted-foreground text-sm">
				Enter the code printed by <code class="font-mono text-xs">lewnotes login --no-browser</code>.
				You will see the device and can approve or deny it on the next screen.
			</p>
		</header>

		{#if checking}
			<p class="text-muted-foreground text-sm">Checking your session…</p>
		{:else}
			<form class="space-y-5" onsubmit={submit}>
				<Field.Field>
					<Field.Label for="user-code">Device code</Field.Label>
					<Input
						id="user-code"
						bind:value={code}
						placeholder="7K4M-9QRT"
						autocomplete="one-time-code"
						spellcheck="false"
						class="font-mono text-lg tracking-[0.2em] uppercase"
					/>
					<Field.Description>
						Codes are single use and expire ten minutes after the CLI asks for one.
					</Field.Description>
				</Field.Field>

				{#if error}
					<Alert.Root variant="destructive">
						<Alert.Title>Code not accepted</Alert.Title>
						<Alert.Description>{error}</Alert.Description>
					</Alert.Root>
				{/if}

				<Button class="w-full" type="submit" disabled={busy || code.trim().length < 8}>
					{busy ? 'Looking up…' : 'Continue'}
				</Button>
			</form>
		{/if}

		<p class="text-muted-foreground text-xs">
			<a class="underline underline-offset-4" href="/notes">Back to notes</a>
		</p>
	</section>
</main>
