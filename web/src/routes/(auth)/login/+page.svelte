<script lang="ts">
	import { goto } from '$app/navigation';

	import { api, ApiError } from '#lib/api';

	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		busy = true;
		try {
			await api.login(fetch, password);
			await goto('/notes');
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
			<p class="text-xs tracking-[0.2em] text-muted uppercase">self-hosted notes</p>
			<h1 class="text-3xl font-semibold tracking-tight">lewnotes</h1>
			<p class="text-sm text-muted">Sign in to the vault on this server.</p>
		</header>

		<form class="space-y-5" onsubmit={submit}>
			<label class="block space-y-2">
				<span class="text-sm font-medium text-neutral-300">Password</span>
				<input
					class="field"
					type="password"
					name="password"
					autocomplete="current-password"
					required
					bind:value={password}
				/>
			</label>

			{#if error}
				<p class="rounded-md border border-rose-900/60 bg-rose-950/40 px-3 py-2 text-sm text-rose-300" role="alert">
					{error}
				</p>
			{/if}

			<button class="btn-primary w-full" type="submit" disabled={busy || password.length === 0}>
				{busy ? 'Signing in…' : 'Sign in'}
			</button>
		</form>

		<p class="text-xs leading-relaxed text-muted">
			First run? Visit <a class="underline underline-offset-4" href="/setup">setup</a> to create the owner
			account.
		</p>
	</section>
</main>
