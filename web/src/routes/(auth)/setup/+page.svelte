<script lang="ts">
	import { goto } from '$app/navigation';

	import { api, ApiError } from '#lib/api';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';

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
			<p class="text-muted-foreground text-xs tracking-[0.2em] uppercase">first run</p>
			<h1 class="text-3xl font-semibold tracking-tight">Create the owner account</h1>
			<p class="text-muted-foreground text-sm leading-relaxed">
				One instance, one owner, one vault. This account protects every interface.
			</p>
		</header>

		<form class="space-y-5" onsubmit={submit}>
			<Field.Field>
				<Field.Label for="setup-instance">Instance name</Field.Label>
				<Input
					id="setup-instance"
					name="instance"
					autocomplete="organization"
					bind:value={instanceName}
				/>
			</Field.Field>

			<Field.Field>
				<Field.Label for="setup-password">Password</Field.Label>
				<Input
					id="setup-password"
					type="password"
					name="password"
					autocomplete="new-password"
					required
					minlength={10}
					bind:value={password}
				/>
				<Field.Description>At least 10 characters. Stored as argon2id.</Field.Description>
			</Field.Field>

			<Field.Field>
				<Field.Label for="setup-confirm">Confirm password</Field.Label>
				<Input
					id="setup-confirm"
					type="password"
					name="confirm"
					autocomplete="new-password"
					required
					bind:value={confirm}
				/>
			</Field.Field>

			{#if error}
				<Alert.Root variant="destructive">
					<Alert.Title>Setup failed</Alert.Title>
					<Alert.Description>{error}</Alert.Description>
				</Alert.Root>
			{/if}

			<Button class="w-full" type="submit" disabled={busy || !password || !confirm}>
				{busy ? 'Creating…' : 'Create account and sign in'}
			</Button>
		</form>
	</section>
</main>
