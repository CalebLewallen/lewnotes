<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';

	import { api, ApiError } from '#lib/api';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';

	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		busy = true;
		try {
			await api.login(fetch, password);
			const next = page.url.searchParams.get('next');
			await goto(next && next.startsWith('/') && !next.startsWith('//') ? next : '/notes');
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not sign in.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Sign in · lewnotes</title></svelte:head>

<main class="flex min-h-dvh items-center justify-center px-6 py-16">
	<section class="w-full max-w-sm space-y-8">
		<header class="space-y-2">
			<p class="text-muted-foreground text-xs tracking-[0.2em] uppercase">self-hosted notes</p>
			<h1 class="text-3xl font-semibold tracking-tight">lewnotes</h1>
			<p class="text-muted-foreground text-sm">Sign in to the vault on this server.</p>
		</header>

		<form class="space-y-5" onsubmit={submit}>
			<Field.Field>
				<Field.Label for="login-password">Password</Field.Label>
				<Input
					id="login-password"
					type="password"
					name="password"
					autocomplete="current-password"
					required
					bind:value={password}
				/>
			</Field.Field>

			{#if error}
				<Alert.Root variant="destructive">
					<Alert.Title>Sign in failed</Alert.Title>
					<Alert.Description>{error}</Alert.Description>
				</Alert.Root>
			{/if}

			<Button class="w-full" type="submit" disabled={busy || password.length === 0}>
				{busy ? 'Signing in…' : 'Sign in'}
			</Button>
		</form>

		<p class="text-muted-foreground text-xs leading-relaxed">
			First run? Visit <a class="underline underline-offset-4" href="/setup">setup</a> to create the
			owner account.
		</p>
	</section>
</main>
