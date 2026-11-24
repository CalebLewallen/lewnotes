<script lang="ts">
	import { goto } from '$app/navigation';

	import { api, ApiError } from '#lib/api';

	let instanceName = $state('home');
	let password = $state('');
	let confirm = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		if (password !== confirm) {
			error = 'The passwords do not match.';
			return;
		}
		busy = true;
		try {
			await api.setup(fetch, password, instanceName.trim() || 'home');
			await goto('/notes');
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not finish setup.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Set up lewnotes</title></svelte:head>

<main class="flex min-h-dvh items-center justify-center px-6 py-16">
	<section class="w-full max-w-md space-y-8">
		<header class="space-y-2">
			<p class="text-xs tracking-[0.2em] text-muted uppercase">first run</p>
			<h1 class="text-3xl font-semibold tracking-tight">Create the owner account</h1>
			<p class="text-sm leading-relaxed text-muted">
				One instance, one owner, one vault. This account protects every interface.
			</p>
		</header>

		<form class="space-y-5" onsubmit={submit}>
			<label class="block space-y-2">
				<span class="text-sm font-medium text-neutral-300">Instance name</span>
				<input class="field" name="instance" autocomplete="organization" bind:value={instanceName} />
			</label>

			<label class="block space-y-2">
				<span class="text-sm font-medium text-neutral-300">Password</span>
				<input
					class="field"
					type="password"
					name="password"
					autocomplete="new-password"
					required
					minlength="10"
					bind:value={password}
				/>
				<span class="text-xs text-muted">At least 10 characters. Stored as argon2id.</span>
			</label>

			<label class="block space-y-2">
				<span class="text-sm font-medium text-neutral-300">Confirm password</span>
				<input
					class="field"
					type="password"
					name="confirm"
					autocomplete="new-password"
					required
					bind:value={confirm}
				/>
			</label>

			{#if error}
				<p class="rounded-md border border-rose-900/60 bg-rose-950/40 px-3 py-2 text-sm text-rose-300" role="alert">
					{error}
				</p>
			{/if}

			<button class="btn-primary w-full" type="submit" disabled={busy || !password || !confirm}>
				{busy ? 'Creating…' : 'Create account and sign in'}
			</button>
		</form>
	</section>
</main>
